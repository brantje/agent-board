package postgres

import (
	"context"
	"errors"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func delegatedParentTerminalTx(ctx context.Context, tx pgx.Tx, child store.Run) (bool, error) {
	delegation, err := scanDelegation(tx.QueryRow(ctx, `
		SELECT `+delegationSelectColumns+`
		FROM delegations
		WHERE project_id=$1 AND delegated_run_id=$2
		FOR UPDATE
	`, child.ProjectID, child.ID))
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if delegation.IssueID != child.IssueID {
		return false, store.ErrConflict
	}
	parent, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, delegation.ProjectID, delegation.ParentRunID))
	if err != nil {
		return false, err
	}
	if parent.IssueID != child.IssueID || parent.WorkspaceID != child.WorkspaceID || parent.AgentID == nil || *parent.AgentID != delegation.ParentAgentID {
		return false, store.ErrConflict
	}
	switch parent.Status {
	case "COMPLETED", "FAILED", "CANCELLED":
		return true, nil
	default:
		return false, nil
	}
}

func inactiveRunExecutionOccupiedTx(ctx context.Context, tx pgx.Tx, projectID, runID string) (bool, error) {
	var activeSessions, claimedJobs int
	if err := tx.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM execution_sessions WHERE project_id=$1 AND run_id=$2 AND status IN ('PENDING','STARTING','RUNNING')),
		  (SELECT count(*) FROM scheduler_jobs WHERE project_id=$1 AND run_id=$2 AND state='CLAIMED')
	`, projectID, runID).Scan(&activeSessions, &claimedJobs); err != nil {
		return false, err
	}
	return activeSessions != 0 || claimedJobs != 0, nil
}
