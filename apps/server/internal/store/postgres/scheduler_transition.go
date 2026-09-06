package postgres

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) transitionAdmittedJob(ctx context.Context, input store.SchedulerTransition) (store.Run, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.JobID) == "" ||
		strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.LeaseToken) == "" {
		return store.Run{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.Run{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := lockFencedRun(ctx, tx, input.ProjectID, input.JobID, input.RunID, input.LeaseToken)
	if err != nil {
		return store.Run{}, err
	}
	if !validSchedulerRunTransition(current.Status, input.RunStatus) {
		return store.Run{}, store.ErrConflict
	}

	release, jobState, terminal, err := schedulerTransitionEffects(input.RunStatus)
	if err != nil {
		return store.Run{}, err
	}
	if input.RunStatus == "FAILED" && (input.FailureReason == nil || strings.TrimSpace(*input.FailureReason) == "") {
		return store.Run{}, store.ErrInvalidArgument
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		UPDATE runs
		SET status=$3,
		    queue_reason=NULL,
		    failure_reason=CASE WHEN $3='FAILED' THEN $4 ELSE failure_reason END,
		    completed_at=CASE WHEN $5 THEN now() ELSE completed_at END,
		    updated_at=now()
		WHERE project_id=$1 AND id=$2
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, input.ProjectID, input.RunID, input.RunStatus, input.FailureReason, terminal))
	if err != nil {
		return store.Run{}, err
	}

	if input.RunStatus == "WAITING_FOR_INPUT" {
		command, err := tx.Exec(ctx, `
			UPDATE issues
			SET status=CASE WHEN status='DONE' THEN status ELSE 'BLOCKED' END,
			    updated_at=CASE WHEN status='DONE' THEN updated_at ELSE now() END
			WHERE project_id=$1 AND id=$2
		`, run.ProjectID, run.IssueID)
		if err != nil {
			return store.Run{}, err
		}
		if command.RowsAffected() != 1 {
			return store.Run{}, store.ErrConflict
		}
	}

	if release {
		if _, err := tx.Exec(ctx, `DELETE FROM scheduler_capacity_reservations WHERE project_id=$1 AND job_id=$2`, input.ProjectID, input.JobID); err != nil {
			return store.Run{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE scheduler_jobs
			SET state=$4, wait_reason=NULL, updated_at=now()
			WHERE id=$2 AND project_id=$1 AND run_id=$3 AND state='CLAIMED'
		`, input.ProjectID, input.JobID, input.RunID, jobState); err != nil {
			return store.Run{}, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM scheduler_leases WHERE job_id=$1 AND lease_token=$2`, input.JobID, input.LeaseToken); err != nil {
			return store.Run{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return store.Run{}, err
	}
	return run, nil
}

func lockFencedRun(ctx context.Context, tx pgx.Tx, projectID, jobID, runID, leaseToken string) (store.Run, error) {
	return scanRun(tx.QueryRow(ctx, `
		SELECT run.id::text, run.project_id::text, run.issue_id::text, run.workspace_id::text, run.agent_id::text, run.attempt,
		       run.status, run.queue_reason, run.failure_reason, run.created_at, run.started_at, run.completed_at, run.updated_at
		FROM scheduler_leases AS lease
		JOIN scheduler_jobs AS job ON job.id=lease.job_id
		JOIN runs AS run ON run.project_id=job.project_id AND run.id=job.run_id
		WHERE job.project_id=$1 AND job.id=$2 AND job.run_id=$3
		  AND job.state='CLAIMED' AND lease.lease_token=$4 AND lease.expires_at > now()
		FOR UPDATE OF lease, job, run
	`, projectID, jobID, runID, leaseToken))
}

func validSchedulerRunTransition(from, to string) bool {
	if from == to && to == "RUNNING" {
		return true
	}
	switch from {
	case "STARTING":
		switch to {
		case "RUNNING", "WAITING_FOR_INPUT", "PAUSED", "READY_FOR_REVIEW", "COMPLETED", "FAILED", "CANCELLED":
			return true
		}
	case "RUNNING":
		switch to {
		case "WAITING_FOR_INPUT", "PAUSED", "READY_FOR_REVIEW", "COMPLETED", "FAILED", "CANCELLED":
			return true
		}
	}
	return false
}

func schedulerTransitionEffects(status string) (release bool, jobState string, terminal bool, err error) {
	switch status {
	case "RUNNING":
		return false, "CLAIMED", false, nil
	case "WAITING_FOR_INPUT", "PAUSED", "READY_FOR_REVIEW":
		return true, "DONE", false, nil
	case "COMPLETED":
		return true, "DONE", true, nil
	case "FAILED":
		return true, "FAILED", true, nil
	case "CANCELLED":
		return true, "CANCELLED", true, nil
	default:
		return false, "", false, store.ErrInvalidArgument
	}
}
