package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const pendingWorkspacePrefix = "pending://workspace/"

func (s *Store) AssignIssue(ctx context.Context, projectID, issueID, agentID string) (store.Issue, store.Run, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	issue, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, projectID, issueID)
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}
	if issue.Status == "DONE" {
		return store.Issue{}, store.Run{}, store.ErrConflict
	}
	if err := verifyRunnableAgent(ctx, tx, projectID, agentID); err != nil {
		return store.Issue{}, store.Run{}, err
	}

	active, err := latestActiveRun(ctx, tx, projectID, issueID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Issue{}, store.Run{}, err
	}
	if err == nil {
		if active.AgentID != nil && *active.AgentID == agentID {
			if err := tx.Commit(ctx); err != nil {
				return store.Issue{}, store.Run{}, err
			}
			return issue, active, nil
		}
		if active.Status != "QUEUED" {
			return store.Issue{}, store.Run{}, store.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE runs SET status='CANCELLED', completed_at=now(), updated_at=now() WHERE project_id=$1 AND id=$2`, projectID, active.ID); err != nil {
			return store.Issue{}, store.Run{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE scheduler_jobs SET state='CANCELLED', updated_at=now() WHERE project_id=$1 AND run_id=$2 AND state='QUEUED'`, projectID, active.ID); err != nil {
			return store.Issue{}, store.Run{}, err
		}
	}

	workspace, err := workspaceForAssignment(ctx, tx, projectID, issueID, repositoryPath, defaultBranch)
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}

	var attempt int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(attempt), 0) + 1 FROM runs WHERE issue_id=$1`, issueID).Scan(&attempt); err != nil {
		return store.Issue{}, store.Run{}, err
	}

	issue, err = scanIssue(tx.QueryRow(ctx, `
        UPDATE issues
        SET assigned_agent_id=$3, status='IN_PROGRESS', updated_at=now()
        WHERE project_id=$1 AND id=$2
        RETURNING id::text, project_id::text, title, description, status, assigned_agent_id::text, created_at, updated_at
    `, projectID, issueID, agentID))
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}

	run, err := scanRun(tx.QueryRow(ctx, `
        INSERT INTO runs (project_id, issue_id, workspace_id, agent_id, attempt, status)
        VALUES ($1, $2, $3, $4, $5, 'QUEUED')
        RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
    `, projectID, issueID, workspace.ID, agentID, attempt))
	if err != nil {
		return store.Issue{}, store.Run{}, err
	}

	if _, err := tx.Exec(ctx, `
        INSERT INTO scheduler_jobs (project_id, run_id, kind, state, idempotency_key)
        VALUES ($1, $2, 'START', 'QUEUED', $3)
    `, projectID, run.ID, "run:"+run.ID+":start"); err != nil {
		return store.Issue{}, store.Run{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return store.Issue{}, store.Run{}, err
	}
	return issue, run, nil
}

func lockAssignmentIssue(ctx context.Context, tx pgx.Tx, projectID, issueID string) (store.Issue, string, string, error) {
	var issue store.Issue
	var repositoryPath, defaultBranch string
	err := tx.QueryRow(ctx, `
        SELECT issue.id::text, issue.project_id::text, issue.title, issue.description, issue.status,
               issue.assigned_agent_id::text, issue.created_at, issue.updated_at,
               project.repository_path, project.default_branch
        FROM issues AS issue
        JOIN projects AS project ON project.id=issue.project_id
        WHERE issue.project_id=$1 AND issue.id=$2
        FOR UPDATE OF issue
    `, projectID, issueID).Scan(
		&issue.ID, &issue.ProjectID, &issue.Title, &issue.Description, &issue.Status,
		&issue.AssignedAgentID, &issue.CreatedAt, &issue.UpdatedAt,
		&repositoryPath, &defaultBranch,
	)
	if err != nil {
		return store.Issue{}, "", "", notFound(err)
	}
	return issue, repositoryPath, defaultBranch, nil
}

func verifyRunnableAgent(ctx context.Context, tx pgx.Tx, projectID, agentID string) error {
	var agentState, providerHealth, runtimeHealth, runtimeKind, runtimeImage string
	var executorEnabled, modelEnabled, providerEnabled, runtimeEnabled bool
	err := tx.QueryRow(ctx, `
        SELECT agent.state, executor.enabled, model.enabled, provider.enabled, provider.health_status,
               runtime.enabled, runtime.health_status, runtime.kind, runtime.image
        FROM agents AS agent
        JOIN executor_profiles AS executor ON executor.id=agent.executor_profile_id
        JOIN model_profiles AS model ON model.id=executor.model_profile_id
        JOIN providers AS provider ON provider.id=model.provider_id
        JOIN runtimes AS runtime ON runtime.id=executor.runtime_id
        WHERE agent.id=$2
          AND (agent.project_id IS NULL OR agent.project_id=$1)
          AND (executor.project_id IS NULL OR executor.project_id=$1)
          AND (model.project_id IS NULL OR model.project_id=$1)
          AND (runtime.project_id IS NULL OR runtime.project_id=$1)
        FOR SHARE OF agent, executor, model, provider, runtime
    `, projectID, agentID).Scan(
		&agentState, &executorEnabled, &modelEnabled, &providerEnabled, &providerHealth,
		&runtimeEnabled, &runtimeHealth, &runtimeKind, &runtimeImage,
	)
	if err != nil {
		return notFound(err)
	}
	if agentState != "ENABLED" || !executorEnabled || !modelEnabled || !providerEnabled || providerHealth == "UNHEALTHY" ||
		!runtimeEnabled || runtimeHealth == "UNHEALTHY" || runtimeKind != "docker" || strings.TrimSpace(runtimeImage) == "" {
		return store.ErrConflict
	}
	return nil
}

func latestActiveRun(ctx context.Context, tx pgx.Tx, projectID, issueID string) (store.Run, error) {
	return scanRun(tx.QueryRow(ctx, `
        SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
        FROM runs
        WHERE project_id=$1 AND issue_id=$2 AND status IN ('QUEUED','STARTING','RUNNING','WAITING_FOR_INPUT','PAUSED')
        ORDER BY attempt DESC
        LIMIT 1
        FOR UPDATE
    `, projectID, issueID))
}

func workspaceForAssignment(ctx context.Context, tx pgx.Tx, projectID, issueID, repositoryPath, defaultBranch string) (store.Workspace, error) {
	workspace, err := scanWorkspace(tx.QueryRow(ctx, `
        SELECT id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, bootstrap_status, created_at, updated_at
        FROM workspaces WHERE project_id=$1 AND issue_id=$2
    `, projectID, issueID))
	if err == nil {
		return workspace, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Workspace{}, err
	}

	// #6 reserves only the durable identity. Issue #7 replaces the pending URI
	// with the validated filesystem path when it materializes the Git checkout.
	return scanWorkspace(tx.QueryRow(ctx, `
        INSERT INTO workspaces (project_id, issue_id, path, repository_path, base_branch, working_branch, bootstrap_status)
        VALUES ($1, $2, $3, $4, $5, $6, 'PENDING')
        RETURNING id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, bootstrap_status, created_at, updated_at
    `, projectID, issueID, pendingWorkspacePrefix+issueID, repositoryPath, defaultBranch, "agent-board/issue-"+issueID))
}
