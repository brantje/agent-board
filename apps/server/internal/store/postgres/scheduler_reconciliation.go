package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const schedulerReconciliationRetryReason = "reconciliation_retry"

func (s *Store) claimExpiredJobForReconciliation(ctx context.Context, ownerID string, leaseDuration time.Duration) (*store.SchedulerAdmission, error) {
	leaseMicros := leaseDuration.Microseconds()
	if strings.TrimSpace(ownerID) == "" || leaseMicros <= 0 {
		return nil, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	job, run, agentID, modelProfileID, oldLease, err := lockExpiredReconciliationCandidate(ctx, tx)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if run.Status == "QUEUED" && (agentID == "" || modelProfileID == "") {
		if err := resetInvalidQueuedClaim(ctx, tx, job, oldLease); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}

	lease, err := scanSchedulerLease(tx.QueryRow(ctx, `
		UPDATE scheduler_leases
		SET owner_id=$2,
		    lease_token=gen_random_uuid(),
		    acquired_at=now(),
		    expires_at=now() + ($3::bigint * interval '1 microsecond')
		WHERE job_id=$1 AND lease_token=$4 AND expires_at <= now()
		RETURNING job_id::text, owner_id, lease_token::text, acquired_at, expires_at
	`, job.ID, ownerID, leaseMicros, oldLease.LeaseToken))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &store.SchedulerAdmission{
		Job:            job,
		Lease:          lease,
		Run:            run,
		AgentID:        agentID,
		ModelProfileID: modelProfileID,
	}, nil
}

func lockExpiredReconciliationCandidate(ctx context.Context, tx pgx.Tx) (store.SchedulerJob, store.Run, string, string, store.SchedulerLease, error) {
	var job store.SchedulerJob
	var run store.Run
	var lease store.SchedulerLease
	var agentID, modelProfileID *string
	err := tx.QueryRow(ctx, `
		SELECT
			job.id::text, job.project_id::text, job.run_id::text, job.kind, job.state, job.wait_reason, job.idempotency_key, job.available_at, job.created_at, job.updated_at,
			run.id::text, run.project_id::text, run.issue_id::text, run.workspace_id::text, run.agent_id::text, run.attempt, run.status, run.queue_reason, run.failure_reason, run.created_at, run.started_at, run.completed_at, run.updated_at,
			agent.id::text, model.id::text,
			lease.job_id::text, lease.owner_id, lease.lease_token::text, lease.acquired_at, lease.expires_at
		FROM scheduler_leases AS lease
		JOIN scheduler_jobs AS job ON job.id=lease.job_id
		JOIN runs AS run ON run.project_id=job.project_id AND run.id=job.run_id
		LEFT JOIN agents AS agent
		  ON agent.id=run.agent_id
		 AND (agent.project_id IS NULL OR agent.project_id=run.project_id)
		LEFT JOIN executor_profiles AS executor
		  ON executor.id=agent.executor_profile_id
		 AND (executor.project_id IS NULL OR executor.project_id=run.project_id)
		LEFT JOIN model_profiles AS model
		  ON model.id=executor.model_profile_id
		 AND (model.project_id IS NULL OR model.project_id=run.project_id)
		WHERE job.state='CLAIMED' AND lease.expires_at <= now()
		ORDER BY lease.expires_at, job.created_at, job.id
		FOR UPDATE OF lease, job, run SKIP LOCKED
		LIMIT 1
	`).Scan(
		&job.ID, &job.ProjectID, &job.RunID, &job.Kind, &job.State, &job.WaitReason, &job.IdempotencyKey, &job.AvailableAt, &job.CreatedAt, &job.UpdatedAt,
		&run.ID, &run.ProjectID, &run.IssueID, &run.WorkspaceID, &run.AgentID, &run.Attempt, &run.Status, &run.QueueReason, &run.FailureReason, &run.CreatedAt, &run.StartedAt, &run.CompletedAt, &run.UpdatedAt,
		&agentID, &modelProfileID,
		&lease.JobID, &lease.OwnerID, &lease.LeaseToken, &lease.AcquiredAt, &lease.ExpiresAt,
	)
	if err != nil {
		return store.SchedulerJob{}, store.Run{}, "", "", store.SchedulerLease{}, notFound(err)
	}
	var resolvedAgentID, resolvedModelProfileID string
	if agentID != nil {
		resolvedAgentID = *agentID
	}
	if modelProfileID != nil {
		resolvedModelProfileID = *modelProfileID
	}
	return job, run, resolvedAgentID, resolvedModelProfileID, lease, nil
}

func resetInvalidQueuedClaim(ctx context.Context, tx pgx.Tx, job store.SchedulerJob, lease store.SchedulerLease) error {
	if _, err := tx.Exec(ctx, `
		UPDATE scheduler_jobs
		SET state='QUEUED', wait_reason=$2,
		    available_at=now() + ($3::bigint * interval '1 microsecond'), updated_at=now()
		WHERE id=$1 AND state='CLAIMED'
	`, job.ID, schedulerConfigurationWaitReason, schedulerConfigurationBackoff.Microseconds()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE runs
		SET queue_reason=$3, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='QUEUED'
	`, job.ProjectID, job.RunID, schedulerConfigurationWaitReason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM scheduler_capacity_reservations WHERE project_id=$1 AND job_id=$2`, job.ProjectID, job.ID); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `DELETE FROM scheduler_leases WHERE job_id=$1 AND lease_token=$2`, job.ID, lease.LeaseToken)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) resolveReconciliation(ctx context.Context, input store.SchedulerReconciliation) (store.Run, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.JobID) == "" ||
		strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.LeaseToken) == "" {
		return store.Run{}, store.ErrInvalidArgument
	}
	if input.Outcome == "" {
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

	switch input.Outcome {
	case store.SchedulerReconciliationActive, store.SchedulerReconciliationUnknown:
		if err := tx.Commit(ctx); err != nil {
			return store.Run{}, err
		}
		return current, nil
	case store.SchedulerReconciliationRetry:
		run, err := scanRun(tx.QueryRow(ctx, `
			UPDATE runs
			SET status='QUEUED', queue_reason=$3, failure_reason=NULL, completed_at=NULL, updated_at=now()
			WHERE project_id=$1 AND id=$2 AND status IN ('STARTING', 'RUNNING')
			RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		`, input.ProjectID, input.RunID, schedulerReconciliationRetryReason))
		if err != nil {
			return store.Run{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE scheduler_jobs
			SET state='QUEUED', wait_reason=$4, available_at=now(), updated_at=now()
			WHERE project_id=$1 AND id=$2 AND run_id=$3 AND state='CLAIMED'
		`, input.ProjectID, input.JobID, input.RunID, schedulerReconciliationRetryReason); err != nil {
			return store.Run{}, err
		}
		if err := releaseReconciledOwnership(ctx, tx, input.ProjectID, input.JobID, input.LeaseToken); err != nil {
			return store.Run{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return store.Run{}, err
		}
		return run, nil
	case store.SchedulerReconciliationCompleted, store.SchedulerReconciliationFailed, store.SchedulerReconciliationCancelled:
		return s.resolveReconciliationTerminal(ctx, tx, input)
	default:
		return store.Run{}, store.ErrInvalidArgument
	}
}

func (s *Store) resolveReconciliationTerminal(ctx context.Context, tx pgx.Tx, input store.SchedulerReconciliation) (store.Run, error) {
	runStatus := "COMPLETED"
	jobState := "DONE"
	var failureReason *string
	switch input.Outcome {
	case store.SchedulerReconciliationFailed:
		if input.FailureReason == nil || strings.TrimSpace(*input.FailureReason) == "" {
			return store.Run{}, store.ErrInvalidArgument
		}
		runStatus = "FAILED"
		jobState = "FAILED"
		failureReason = input.FailureReason
	case store.SchedulerReconciliationCancelled:
		runStatus = "CANCELLED"
		jobState = "CANCELLED"
	case store.SchedulerReconciliationCompleted:
	default:
		return store.Run{}, store.ErrInvalidArgument
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		UPDATE runs
		SET status=$3, queue_reason=NULL, failure_reason=$4, completed_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status IN ('STARTING', 'RUNNING')
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, input.ProjectID, input.RunID, runStatus, failureReason))
	if err != nil {
		return store.Run{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE scheduler_jobs
		SET state=$4, wait_reason=NULL, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND run_id=$3 AND state='CLAIMED'
	`, input.ProjectID, input.JobID, input.RunID, jobState); err != nil {
		return store.Run{}, err
	}
	if err := releaseReconciledOwnership(ctx, tx, input.ProjectID, input.JobID, input.LeaseToken); err != nil {
		return store.Run{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Run{}, err
	}
	return run, nil
}

func releaseReconciledOwnership(ctx context.Context, tx pgx.Tx, projectID, jobID, leaseToken string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM scheduler_capacity_reservations WHERE project_id=$1 AND job_id=$2`, projectID, jobID); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `DELETE FROM scheduler_leases WHERE job_id=$1 AND lease_token=$2`, jobID, leaseToken)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return store.ErrNotFound
	}
	return nil
}
