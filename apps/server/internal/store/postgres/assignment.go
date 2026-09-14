package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const pendingWorkspacePrefix = "pending://workspace/"

var activeRunStatuses = []string{
	"QUEUED",
	"STARTING",
	"RUNNING",
	"WAITING_FOR_INPUT",
	"PAUSED",
	"READY_FOR_REVIEW",
}

// The caller holds the Issue row lock through commit: policy, readiness,
// pair-scoped duplicate suppression, attempt allocation and enqueue are atomic.
// The returned Event is non-zero only when this call persisted run.created.
func enqueueIssueMutation(ctx context.Context, tx pgx.Tx, issue store.Issue, previousStatus string, assignment bool, repositoryPath, defaultBranch string) (store.Run, store.Event, error) {
	kind := ""
	if issue.AssigneeType != nil {
		kind = *issue.AssigneeType
	}
	if !store.ShouldAutoEnqueueIssue(previousStatus, issue.Status, kind, assignment) || issue.AssigneeID == nil {
		return store.Run{}, store.Event{}, nil
	}
	projectID, issueID, agentID := issue.ProjectID, issue.ID, *issue.AssigneeID
	active, err := activeRunForAgent(ctx, tx, projectID, issueID, agentID)
	if err == nil {
		slog.DebugContext(ctx, "Issue auto enqueue suppressed: active Run", "issue_id", issueID, "agent_id", agentID, "run_id", active.ID)
		return active, store.Event{}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Run{}, store.Event{}, err
	}
	if err := verifyRunnableAgent(ctx, tx, projectID, agentID); err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			slog.DebugContext(ctx, "Issue auto enqueue skipped: Agent unavailable", "issue_id", issueID, "agent_id", agentID)
			return store.Run{}, store.Event{}, nil
		}
		return store.Run{}, store.Event{}, err
	}

	workspace, err := workspaceForAssignment(ctx, tx, projectID, issueID, issue.Key, repositoryPath, defaultBranch)
	if err != nil {
		return store.Run{}, store.Event{}, err
	}

	var attempt int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(attempt), 0) + 1 FROM runs WHERE issue_id=$1`, issueID).Scan(&attempt); err != nil {
		return store.Run{}, store.Event{}, err
	}

	run, err := scanRun(tx.QueryRow(ctx, `
        INSERT INTO runs (project_id, issue_id, workspace_id, agent_id, attempt, status)
        VALUES ($1, $2, $3, $4, $5, 'QUEUED')
        RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
    `, projectID, issueID, workspace.ID, agentID, attempt))
	if err != nil {
		return store.Run{}, store.Event{}, err
	}

	if _, err := tx.Exec(ctx, `
        INSERT INTO scheduler_jobs (project_id, run_id, kind, state, idempotency_key)
        VALUES ($1, $2, 'START', 'QUEUED', $3)
    `, projectID, run.ID, "run:"+run.ID+":start"); err != nil {
		return store.Run{}, store.Event{}, err
	}

	payload, _ := json.Marshal(map[string]any{"status": run.Status, "attempt": run.Attempt})
	event, err := appendEventTx(ctx, tx, store.Event{Type: "run.created", ProjectID: projectID, IssueID: &issueID, RunID: &run.ID, AgentID: &agentID, WorkspaceID: &workspace.ID, Actor: store.EmptyObject, Payload: payload})
	if err != nil {
		return store.Run{}, store.Event{}, err
	}
	return run, event, nil
}

func lockAssignmentIssue(ctx context.Context, tx pgx.Tx, projectID, issueID string) (store.Issue, string, string, error) {
	var issue store.Issue
	var repositoryPath, defaultBranch, prefix string
	err := tx.QueryRow(ctx, `
        SELECT `+issueSelectColumns+`, p.repository_path, p.default_branch
        FROM issues AS i
        JOIN projects AS p ON p.id=i.project_id
        WHERE i.project_id=$1 AND i.id=$2
        FOR UPDATE OF i
    `, projectID, issueID).Scan(
		&issue.ID, &issue.ProjectID, &issue.Title, &issue.Description, &issue.Status, &issue.Priority,
		&issue.AssigneeType, &issue.AssigneeID, &issue.AssigneeName, &issue.Number, &prefix, &issue.CreatedByType, &issue.CreatedByID, &issue.CreatedByName, &issue.CreatedAt, &issue.UpdatedAt,
		&repositoryPath, &defaultBranch,
	)
	if err != nil {
		return store.Issue{}, "", "", notFound(err)
	}
	issue.Key = store.FormatIssueKey(prefix, issue.Number)
	return issue, repositoryPath, defaultBranch, nil
}

func verifyRunnableAgent(ctx context.Context, tx pgx.Tx, projectID, agentID string) error {
	var agentState, providerHealth string
	var modelEnabled, providerEnabled bool
	err := tx.QueryRow(ctx, `
        SELECT agent.state, model.enabled, provider.enabled, provider.health_status
        FROM agents AS agent
        JOIN model_profiles AS model ON model.id=agent.model_profile_id
        JOIN providers AS provider ON provider.id=model.provider_id
        WHERE agent.id=$2
          AND (agent.project_id IS NULL OR agent.project_id=$1)
          AND (model.project_id IS NULL OR model.project_id=$1)
        FOR SHARE OF agent, model, provider
    `, projectID, agentID).Scan(
		&agentState, &modelEnabled, &providerEnabled, &providerHealth,
	)
	if err != nil {
		return notFound(err)
	}
	if agentState != "ENABLED" || !modelEnabled || !providerEnabled || providerHealth == "UNHEALTHY" {
		return store.ErrConflict
	}
	return nil
}

func activeRunForAgent(ctx context.Context, tx pgx.Tx, projectID, issueID, agentID string) (store.Run, error) {
	run, err := scanRun(tx.QueryRow(ctx, `
        SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
        FROM runs
        WHERE project_id=$1 AND issue_id=$2 AND agent_id=$3 AND status = ANY($4::text[])
        ORDER BY attempt DESC
        LIMIT 1
    `, projectID, issueID, agentID, activeRunStatuses))
	if err != nil {
		return store.Run{}, err
	}
	return run, nil
}

func workspaceForAssignment(ctx context.Context, tx pgx.Tx, projectID, issueID, issueKey, repositoryPath, defaultBranch string) (store.Workspace, error) {
	workspace, err := scanWorkspace(tx.QueryRow(ctx, `
        SELECT id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, current_branch, bootstrap_status, created_at, updated_at
        FROM workspaces WHERE project_id=$1 AND issue_id=$2
    `, projectID, issueID))
	if err == nil {
		return workspace, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Workspace{}, err
	}

	workingBranch := store.WorkingBranchForIssue(issueKey)
	// #6 reserves only the durable identity. Issue #7 replaces the pending URI
	// with the validated filesystem path when it materializes the Git checkout.
	return scanWorkspace(tx.QueryRow(ctx, `
        INSERT INTO workspaces (project_id, issue_id, path, repository_path, base_branch, working_branch, current_branch, bootstrap_status)
        VALUES ($1, $2, $3, $4, $5, $6, $6, 'PENDING')
        RETURNING id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, current_branch, bootstrap_status, created_at, updated_at
    `, projectID, issueID, pendingWorkspacePrefix+issueID, repositoryPath, defaultBranch, workingBranch))
}
