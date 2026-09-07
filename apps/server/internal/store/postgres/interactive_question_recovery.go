package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func retireExpiredInteractiveClaims(ctx context.Context, tx pgx.Tx, projectID, runID string) error {
	rows, err := tx.Query(ctx, `
		SELECT job.id::text
		FROM scheduler_jobs AS job
		LEFT JOIN scheduler_leases AS lease ON lease.job_id=job.id
		WHERE job.project_id=$1 AND job.run_id=$2 AND job.state='CLAIMED'
		  AND (lease.job_id IS NULL OR lease.expires_at <= now())
		FOR UPDATE OF job
	`, projectID, runID)
	if err != nil {
		return err
	}
	jobIDs := make([]string, 0, 1)
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			rows.Close()
			return err
		}
		jobIDs = append(jobIDs, jobID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, jobID := range jobIDs {
		command, err := tx.Exec(ctx, `
			UPDATE scheduler_jobs
			SET state='DONE', wait_reason=NULL, updated_at=now()
			WHERE project_id=$1 AND id=$2 AND run_id=$3 AND state='CLAIMED'
		`, projectID, jobID, runID)
		if err != nil {
			return err
		}
		if command.RowsAffected() != 1 {
			return store.ErrConflict
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM scheduler_capacity_reservations
			WHERE project_id=$1 AND job_id=$2
		`, projectID, jobID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM scheduler_leases WHERE job_id=$1`, jobID); err != nil {
			return err
		}
	}
	return nil
}

func queueBlockingQuestionResume(ctx context.Context, tx pgx.Tx, question store.Question) (store.Run, store.SchedulerJob, error) {
	run, err := scanRun(tx.QueryRow(ctx, `
		UPDATE runs
		SET status='QUEUED', queue_reason=NULL, failure_reason=NULL, completed_at=NULL, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='WAITING_FOR_INPUT'
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, question.ProjectID, question.RunID))
	if err != nil {
		return store.Run{}, store.SchedulerJob{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issues
		SET status='IN_PROGRESS', updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='BLOCKED'
	`, question.ProjectID, question.IssueID); err != nil {
		return store.Run{}, store.SchedulerJob{}, err
	}
	job, err := scanSchedulerJob(tx.QueryRow(ctx, `
		INSERT INTO scheduler_jobs (project_id, run_id, kind, state, idempotency_key, available_at)
		VALUES ($1, $2, 'RESUME', 'QUEUED', $3, now())
		RETURNING id::text, project_id::text, run_id::text, kind, state, wait_reason, idempotency_key, available_at, created_at, updated_at
	`, question.ProjectID, question.RunID, "question:"+question.ID+":resume"))
	if err != nil {
		return store.Run{}, store.SchedulerJob{}, err
	}
	return run, job, nil
}
