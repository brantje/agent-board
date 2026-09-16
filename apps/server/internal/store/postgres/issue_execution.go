package postgres

import (
	"context"
	"errors"
	"log/slog"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

type issueExecutionCandidate struct {
	projectID string
	issueID   string
	agentID   string
}

type issueExecutionCandidateQuerier interface {
	assigneeQuerier
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func resolvedIssueExecutionCandidates(ctx context.Context, q issueExecutionCandidateQuerier, filter store.IssueExecutionFilter) ([]issueExecutionCandidate, error) {
	rows, err := q.Query(ctx, `
		SELECT project_id::text, id::text, assignee_type, assignee_id::text
		FROM issues
		WHERE ($1='' OR project_id::text=$1)
		  AND ($2='' OR (assignee_type='SQUAD' AND assignee_id::text=$2))
		  AND assignee_type IN ('AGENT','SQUAD')
		  AND assignee_id IS NOT NULL
		  AND status <> 'BACKLOG'
		ORDER BY project_id, id
	`, filter.ProjectID, filter.SquadID)
	if err != nil {
		return nil, err
	}
	type ownerCandidate struct {
		projectID string
		issueID   string
		ownerType string
		ownerID   string
	}
	owners := make([]ownerCandidate, 0)
	for rows.Next() {
		var owner ownerCandidate
		if err := rows.Scan(&owner.projectID, &owner.issueID, &owner.ownerType, &owner.ownerID); err != nil {
			rows.Close()
			return nil, err
		}
		owners = append(owners, owner)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	candidates := make([]issueExecutionCandidate, 0, len(owners))
	for _, owner := range owners {
		issue := store.Issue{ProjectID: owner.projectID, ID: owner.issueID, AssigneeType: &owner.ownerType, AssigneeID: &owner.ownerID}
		agentID, executable, err := resolveIssueExecutionAgent(ctx, q, issue)
		if err != nil {
			return nil, err
		}
		if !executable || (filter.AgentID != "" && agentID != filter.AgentID) {
			continue
		}
		matches, err := executionAgentMatchesFilter(ctx, q, owner.projectID, agentID, filter)
		if err != nil {
			return nil, err
		}
		if matches {
			candidates = append(candidates, issueExecutionCandidate{projectID: owner.projectID, issueID: owner.issueID, agentID: agentID})
		}
	}
	return candidates, nil
}

func executionAgentMatchesFilter(ctx context.Context, q assigneeQuerier, projectID, agentID string, filter store.IssueExecutionFilter) (bool, error) {
	if filter.ModelProfileID == "" && filter.ProviderID == "" {
		return true, nil
	}
	var modelProfileID, providerID string
	err := q.QueryRow(ctx, `
		SELECT a.model_profile_id::text, m.provider_id::text
		FROM agents AS a
		JOIN model_profiles AS m ON m.id=a.model_profile_id
		WHERE a.id=$2
		  AND (a.project_id IS NULL OR a.project_id=$1)
		  AND (m.project_id IS NULL OR m.project_id=$1)
	`, projectID, agentID).Scan(&modelProfileID, &providerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if filter.ModelProfileID != "" && modelProfileID != filter.ModelProfileID {
		return false, nil
	}
	if filter.ProviderID != "" && providerID != filter.ProviderID {
		return false, nil
	}
	return true, nil
}

func (s *Store) StartIssueRun(ctx context.Context, projectID, issueID string) (store.Run, store.Event, error) {
	return s.enqueueCurrentIssue(ctx, projectID, issueID, "", true)
}

func (s *Store) GetIssueExecutionState(ctx context.Context, projectID, issueID string) (store.IssueExecutionState, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return store.IssueExecutionState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	var assigneeType, assigneeID *string
	if err := tx.QueryRow(ctx, `
		SELECT status, assignee_type, assignee_id::text
		FROM issues
		WHERE project_id=$1 AND id=$2
	`, projectID, issueID).Scan(&status, &assigneeType, &assigneeID); err != nil {
		return store.IssueExecutionState{}, notFound(err)
	}
	if status == "BACKLOG" {
		return store.IssueExecutionState{State: store.IssueExecutionBacklog}, nil
	}
	issue := store.Issue{ProjectID: projectID, ID: issueID, Status: status, AssigneeType: assigneeType, AssigneeID: assigneeID}
	agentID, executable, err := resolveIssueExecutionAgent(ctx, tx, issue)
	if err != nil {
		return store.IssueExecutionState{}, err
	}
	if !executable {
		return store.IssueExecutionState{State: store.IssueExecutionNotAgentOwned}, nil
	}

	active, err := activeRunForAgent(ctx, tx, projectID, issueID, agentID)
	if err == nil {
		return store.IssueExecutionState{State: store.IssueExecutionActive, ActiveRun: &active}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.IssueExecutionState{}, err
	}
	if err := s.verifyRunnableAgent(ctx, tx, projectID, agentID); err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			return store.IssueExecutionState{State: store.IssueExecutionConfigurationUnavailable}, nil
		}
		return store.IssueExecutionState{}, err
	}
	return store.IssueExecutionState{State: store.IssueExecutionReady, CanStart: true}, nil
}

// Candidate identity is only a hint. Re-read ownership and resolve the current
// execution target under the same Issue lock used by assignment/status mutations
// before creating anything.
func (s *Store) enqueueCurrentIssue(ctx context.Context, projectID, issueID, expectedAgentID string, strict bool) (store.Run, store.Event, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Run{}, store.Event{}, err
	}
	defer tx.Rollback(ctx)
	issue, path, branch, err := lockAssignmentIssue(ctx, tx, projectID, issueID)
	if err != nil {
		return store.Run{}, store.Event{}, err
	}
	kind := ""
	if issue.AssigneeType != nil {
		kind = *issue.AssigneeType
	}
	if !store.ShouldAutoEnqueueIssue(issue.Status, issue.Status, kind, true) || issue.AssigneeID == nil {
		if strict {
			return store.Run{}, store.Event{}, store.ErrConflict
		}
		return store.Run{}, store.Event{}, nil
	}
	agentID, executable, err := resolveIssueExecutionAgent(ctx, tx, issue)
	if err != nil {
		return store.Run{}, store.Event{}, err
	}
	if !executable || (expectedAgentID != "" && agentID != expectedAgentID) {
		if strict {
			return store.Run{}, store.Event{}, store.ErrConflict
		}
		return store.Run{}, store.Event{}, nil
	}
	run, event, err := s.enqueueAssignedIssue(ctx, tx, issue, agentID, path, branch, strict)
	if err != nil {
		return store.Run{}, store.Event{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return store.Run{}, store.Event{}, err
	}
	return run, event, nil
}

// RunnableIssueExecutionScopes evaluates affected Project/resolved-Agent pairs
// using the exact verifyRunnableAgent predicate used by enqueueAssignedIssue. It
// is a readiness snapshot only; Issue status and active-Run policy remain in the
// reconciliation/enqueue path.
func (s *Store) RunnableIssueExecutionScopes(ctx context.Context, filter store.IssueExecutionFilter) ([]store.IssueExecutionScope, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	candidates, err := resolvedIssueExecutionCandidates(ctx, tx, filter)
	if err != nil {
		return nil, err
	}
	unique := make(map[store.IssueExecutionScope]struct{}, len(candidates))
	for _, candidate := range candidates {
		unique[store.IssueExecutionScope{ProjectID: candidate.projectID, AgentID: candidate.agentID}] = struct{}{}
	}
	scopes := make([]store.IssueExecutionScope, 0, len(unique))
	for scope := range unique {
		err = s.verifyRunnableAgent(ctx, tx, scope.ProjectID, scope.AgentID)
		if err == nil {
			scopes = append(scopes, scope)
			continue
		}
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			continue
		}
		return nil, err
	}
	return scopes, nil
}

func (s *Store) ReconcileIssueExecution(ctx context.Context, filter store.IssueExecutionFilter) ([]store.Event, error) {
	candidates, err := resolvedIssueExecutionCandidates(ctx, s.pool, filter)
	if err != nil {
		return nil, err
	}
	var events []store.Event
	var failures []error
	for _, candidate := range candidates {
		_, event, err := s.enqueueCurrentIssue(ctx, candidate.projectID, candidate.issueID, candidate.agentID, false)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			slog.ErrorContext(ctx, "reconcile Issue execution", "issue_id", candidate.issueID, "agent_id", candidate.agentID, "error", err)
			failures = append(failures, err)
			continue
		}
		if event.ID != "" {
			events = append(events, event)
		}
	}
	slog.DebugContext(ctx, "reconciled Issue execution", "candidates", len(candidates), "created", len(events))
	return events, errors.Join(failures...)
}
