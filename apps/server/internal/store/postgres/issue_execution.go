package postgres

import (
	"context"
	"errors"
	"log/slog"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) StartIssueRun(ctx context.Context, projectID, issueID string) (store.Run, store.Event, error) {
	return s.enqueueCurrentIssue(ctx, projectID, issueID, "", true)
}

// Candidate identity is only a hint. Re-read ownership and policy under the same
// Issue lock used by assignment/status mutations before creating anything.
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
	if !store.ShouldAutoEnqueueIssue(issue.Status, issue.Status, kind, true) || issue.AssigneeID == nil || (expectedAgentID != "" && *issue.AssigneeID != expectedAgentID) {
		if strict {
			return store.Run{}, store.Event{}, store.ErrConflict
		}
		return store.Run{}, store.Event{}, nil
	}
	run, event, err := enqueueAssignedIssue(ctx, tx, issue, path, branch, strict)
	if err != nil {
		return store.Run{}, store.Event{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return store.Run{}, store.Event{}, err
	}
	return run, event, nil
}

// RunnableIssueExecutionScopes evaluates affected Agent/project pairs using the
// exact verifyRunnableAgent predicate used by enqueueAssignedIssue. It is a
// readiness snapshot only; Issue status and active-Run policy remain in the
// reconciliation/enqueue path.
func (s *Store) RunnableIssueExecutionScopes(ctx context.Context, filter store.IssueExecutionFilter) ([]store.IssueExecutionScope, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
        SELECT DISTINCT i.project_id::text, a.id::text
        FROM issues i
        JOIN agents a ON i.assignee_type='AGENT' AND i.assignee_id=a.id
        JOIN model_profiles m ON m.id=a.model_profile_id
        WHERE ($1='' OR i.project_id::text=$1)
          AND ($2='' OR a.id::text=$2)
          AND ($3='' OR m.id::text=$3)
          AND ($4='' OR m.provider_id::text=$4)
        ORDER BY 1, 2
    `, filter.ProjectID, filter.AgentID, filter.ModelProfileID, filter.ProviderID)
	if err != nil {
		return nil, err
	}
	type candidate struct{ project, agent string }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.project, &c.agent); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	scopes := make([]store.IssueExecutionScope, 0, len(candidates))
	for _, c := range candidates {
		err = verifyRunnableAgent(ctx, tx, c.project, c.agent)
		if err == nil {
			scopes = append(scopes, store.IssueExecutionScope{ProjectID: c.project, AgentID: c.agent})
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
	rows, err := s.pool.Query(ctx, `SELECT i.project_id::text,i.id::text,a.id::text
 FROM issues i JOIN agents a ON i.assignee_type='AGENT' AND i.assignee_id=a.id
 JOIN model_profiles m ON m.id=a.model_profile_id
 WHERE ($1='' OR i.project_id::text=$1) AND ($2='' OR a.id::text=$2) AND ($3='' OR m.id::text=$3) AND ($4='' OR m.provider_id::text=$4)
 AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.issue_id=i.id AND r.agent_id=a.id AND r.status=ANY($5::text[]))
 ORDER BY i.id`, filter.ProjectID, filter.AgentID, filter.ModelProfileID, filter.ProviderID, activeRunStatuses)
	if err != nil {
		return nil, err
	}
	type candidate struct{ project, issue, agent string }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.project, &c.issue, &c.agent); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var events []store.Event
	var failures []error
	for _, c := range candidates {
		_, event, err := s.enqueueCurrentIssue(ctx, c.project, c.issue, c.agent, false)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			slog.ErrorContext(ctx, "reconcile Issue execution", "issue_id", c.issue, "agent_id", c.agent, "error", err)
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
