package postgres

import (
	"context"
	"errors"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func resolveExpiredBlockingQuestion(ctx context.Context, tx pgx.Tx, job store.SchedulerJob, run store.Run, lease store.SchedulerLease) (bool, error) {
	if run.Status != "STARTING" && run.Status != "RUNNING" {
		return false, nil
	}

	var questionID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM questions
		WHERE project_id=$1 AND run_id=$2 AND blocking AND status='OPEN'
		ORDER BY created_at, id
		LIMIT 1
		FOR UPDATE
	`, run.ProjectID, run.ID).Scan(&questionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE runs
		SET status='WAITING_FOR_INPUT', queue_reason=NULL, failure_reason=NULL, completed_at=NULL, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status IN ('STARTING', 'RUNNING')
	`, run.ProjectID, run.ID); err != nil {
		return false, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE issues
		SET status='BLOCKED', updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status <> 'DONE'
	`, run.ProjectID, run.IssueID)
	if err != nil {
		return false, err
	}
	if command.RowsAffected() != 1 {
		return false, store.ErrConflict
	}
	command, err = tx.Exec(ctx, `
		UPDATE scheduler_jobs
		SET state='DONE', wait_reason=NULL, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND run_id=$3 AND state='CLAIMED'
	`, job.ProjectID, job.ID, job.RunID)
	if err != nil {
		return false, err
	}
	if command.RowsAffected() != 1 {
		return false, store.ErrNotFound
	}
	if err := releaseReconciledOwnership(ctx, tx, run.ProjectID, job.ID, lease.LeaseToken); err != nil {
		return false, err
	}
	return true, nil
}
