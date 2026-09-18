package postgres

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CancelInactiveRun(ctx context.Context, projectID, runID string) (store.RunCancellationResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(runID) == "" {
		return store.RunCancellationResult{}, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.RunCancellationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, projectID, runID))
	if err != nil {
		return store.RunCancellationResult{}, err
	}
	if current.Status != "QUEUED" && current.Status != "PAUSED" {
		return store.RunCancellationResult{}, store.ErrConflict
	}

	var activeSessions, claimedJobs int
	if err := tx.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM execution_sessions WHERE project_id=$1 AND run_id=$2 AND status IN ('PENDING','STARTING','RUNNING')),
		  (SELECT count(*) FROM scheduler_jobs WHERE project_id=$1 AND run_id=$2 AND state='CLAIMED')
	`, projectID, runID).Scan(&activeSessions, &claimedJobs); err != nil {
		return store.RunCancellationResult{}, err
	}
	if activeSessions != 0 || claimedJobs != 0 {
		return store.RunCancellationResult{}, store.ErrConflict
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		UPDATE runs
		SET status='CANCELLED', queue_reason=NULL, completed_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status IN ('QUEUED','PAUSED')
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		          status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, projectID, runID))
	if err != nil {
		return store.RunCancellationResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE scheduler_jobs
		SET state='CANCELLED', wait_reason=NULL, updated_at=now()
		WHERE project_id=$1 AND run_id=$2 AND state='QUEUED'
	`, projectID, runID); err != nil {
		return store.RunCancellationResult{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM scheduler_capacity_reservations WHERE project_id=$1 AND run_id=$2`, projectID, runID); err != nil {
		return store.RunCancellationResult{}, err
	}
	payload, err := json.Marshal(map[string]any{"status": "CANCELLED"})
	if err != nil {
		return store.RunCancellationResult{}, err
	}
	issueID, workspaceID := run.IssueID, run.WorkspaceID
	event, err := appendEventTx(ctx, tx, store.Event{
		Type: "run.cancelled", ProjectID: run.ProjectID, IssueID: &issueID, RunID: &run.ID,
		AgentID: run.AgentID, WorkspaceID: &workspaceID, Actor: store.EmptyObject, Payload: payload,
	})
	if err != nil {
		return store.RunCancellationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.RunCancellationResult{}, err
	}
	return store.RunCancellationResult{Run: run, Event: event}, nil
}
