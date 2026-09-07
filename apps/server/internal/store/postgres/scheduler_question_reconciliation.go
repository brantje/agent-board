package postgres

import (
	"context"
	"errors"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func resolveExpiredBlockingQuestion(ctx context.Context, tx pgx.Tx, job store.SchedulerJob, run store.Run, lease store.SchedulerLease) (bool, error) {
	if run.Status == "WAITING_FOR_INPUT" {
		question, err := scanQuestion(tx.QueryRow(ctx, `
			SELECT q.id::text, q.project_id::text, q.issue_id::text, q.run_id::text, q.prompt, q.kind, q.options, q.recommendation, q.blocking, q.status, q.created_at, q.answered_at
			FROM questions AS q
			JOIN engine_question_bindings AS binding
			  ON binding.project_id=q.project_id
			 AND binding.run_id=q.run_id
			 AND binding.question_id=q.id
			WHERE q.project_id=$1 AND q.run_id=$2 AND q.blocking AND q.status='ANSWERED'
			  AND binding.state='ANSWERED'
			  AND NOT EXISTS (
				SELECT 1
				FROM engine_question_bindings AS pending
				WHERE pending.project_id=q.project_id AND pending.run_id=q.run_id AND pending.state='OPEN'
			  )
			ORDER BY q.answered_at, q.id
			LIMIT 1
			FOR UPDATE OF q, binding
		`, run.ProjectID, run.ID))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return false, nil
			}
			return false, err
		}

		command, err := tx.Exec(ctx, `
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
		if _, _, err := queueBlockingQuestionResume(ctx, tx, question); err != nil {
			return false, err
		}
		return true, nil
	}

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
		SET status=CASE WHEN status='DONE' THEN status ELSE 'BLOCKED' END,
		    updated_at=CASE WHEN status='DONE' THEN updated_at ELSE now() END
		WHERE project_id=$1 AND id=$2
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
