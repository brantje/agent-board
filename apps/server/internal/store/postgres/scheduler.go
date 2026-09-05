package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const (
	schedulerConfigurationWaitReason = "configuration_unavailable"
	schedulerConfigurationBackoff    = time.Second
)

var errSchedulerConfigurationUnavailable = errors.New("scheduler configuration unavailable")

func (s *Store) EnqueueJob(ctx context.Context, input store.SchedulerJob) (store.SchedulerJob, error) {
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return store.SchedulerJob{}, store.ErrInvalidArgument
	}
	kind := input.Kind
	if kind == "" {
		kind = "START"
	}
	availableAt := input.AvailableAt
	if availableAt.IsZero() {
		availableAt = time.Now()
	}
	job, err := scanSchedulerJob(s.pool.QueryRow(ctx, `
		INSERT INTO scheduler_jobs (project_id, run_id, kind, state, wait_reason, idempotency_key, available_at)
		SELECT $1, $2, $3, 'QUEUED', $4, $5, $6
		FROM runs AS run
		JOIN agents AS agent
		  ON agent.id=run.agent_id
		 AND (agent.project_id IS NULL OR agent.project_id=run.project_id)
		JOIN executor_profiles AS executor
		  ON executor.id=agent.executor_profile_id
		 AND (executor.project_id IS NULL OR executor.project_id=run.project_id)
		JOIN model_profiles AS model
		  ON model.id=executor.model_profile_id
		 AND (model.project_id IS NULL OR model.project_id=run.project_id)
		WHERE run.project_id=$1 AND run.id=$2
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id::text, project_id::text, run_id::text, kind, state, wait_reason, idempotency_key, available_at, created_at, updated_at
	`, input.ProjectID, input.RunID, kind, input.WaitReason, input.IdempotencyKey, availableAt))
	if err == nil {
		return job, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.SchedulerJob{}, err
	}
	existing, err := scanSchedulerJob(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, run_id::text, kind, state, wait_reason, idempotency_key, available_at, created_at, updated_at
		FROM scheduler_jobs
		WHERE idempotency_key = $1
	`, input.IdempotencyKey))
	if err == nil {
		if existing.ProjectID != input.ProjectID || existing.RunID != input.RunID || existing.Kind != kind {
			return store.SchedulerJob{}, store.ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.SchedulerJob{}, err
	}

	var runExists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM runs WHERE project_id=$1 AND id=$2)`, input.ProjectID, input.RunID).Scan(&runExists); err != nil {
		return store.SchedulerJob{}, err
	}
	if !runExists {
		return store.SchedulerJob{}, store.ErrNotFound
	}
	return store.SchedulerJob{}, store.ErrConflict
}

func (s *Store) AdmitNextJob(ctx context.Context, ownerID string, leaseDuration, capacityBackoff time.Duration) (*store.SchedulerAdmission, error) {
	leaseMicros := leaseDuration.Microseconds()
	backoffMicros := capacityBackoff.Microseconds()
	if strings.TrimSpace(ownerID) == "" || leaseMicros <= 0 || backoffMicros <= 0 {
		return nil, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	job, run, agentID, modelProfileID, err := lockNextAdmissionCandidate(ctx, tx)
	if errors.Is(err, errSchedulerConfigurationUnavailable) {
		if err := deferQueuedJob(ctx, tx, job, schedulerConfigurationWaitReason, backoffMicros); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	agentLimit, modelLimit, err := lockAdmissionResources(ctx, tx, agentID, modelProfileID)
	if err != nil {
		return nil, err
	}

	agentUsed, err := countCapacityReservations(ctx, tx, "AGENT", agentID)
	if err != nil {
		return nil, err
	}
	if agentUsed >= agentLimit {
		if err := deferQueuedJob(ctx, tx, job, store.SchedulerWaitAgentCapacity, backoffMicros); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}

	if modelLimit != nil {
		modelUsed, err := countCapacityReservations(ctx, tx, "MODEL_PROFILE", modelProfileID)
		if err != nil {
			return nil, err
		}
		if modelUsed >= *modelLimit {
			if err := deferQueuedJob(ctx, tx, job, store.SchedulerWaitModelCapacity, backoffMicros); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return nil, nil
		}
	}

	if err := insertCapacityReservation(ctx, tx, job, run, "AGENT", agentID); err != nil {
		return nil, err
	}
	if err := insertCapacityReservation(ctx, tx, job, run, "MODEL_PROFILE", modelProfileID); err != nil {
		return nil, err
	}

	lease, err := scanSchedulerLease(tx.QueryRow(ctx, `
		INSERT INTO scheduler_leases (job_id, owner_id, expires_at)
		VALUES ($1, $2, now() + ($3::bigint * interval '1 microsecond'))
		RETURNING job_id::text, owner_id, lease_token::text, acquired_at, expires_at
	`, job.ID, ownerID, leaseMicros))
	if err != nil {
		return nil, err
	}

	job, err = scanSchedulerJob(tx.QueryRow(ctx, `
		UPDATE scheduler_jobs
		SET state='CLAIMED', wait_reason=NULL, updated_at=now()
		WHERE id=$1 AND project_id=$2 AND run_id=$3 AND state='QUEUED'
		RETURNING id::text, project_id::text, run_id::text, kind, state, wait_reason, idempotency_key, available_at, created_at, updated_at
	`, job.ID, job.ProjectID, job.RunID))
	if err != nil {
		return nil, err
	}

	run, err = scanRun(tx.QueryRow(ctx, `
		UPDATE runs
		SET status='STARTING', queue_reason=NULL, started_at=COALESCE(started_at, now()), updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='QUEUED'
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, run.ProjectID, run.ID))
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

func lockNextAdmissionCandidate(ctx context.Context, tx pgx.Tx) (store.SchedulerJob, store.Run, string, string, error) {
	var job store.SchedulerJob
	var run store.Run
	var agentID, modelProfileID *string
	err := tx.QueryRow(ctx, `
		SELECT
			job.id::text, job.project_id::text, job.run_id::text, job.kind, job.state, job.wait_reason, job.idempotency_key, job.available_at, job.created_at, job.updated_at,
			run.id::text, run.project_id::text, run.issue_id::text, run.workspace_id::text, run.agent_id::text, run.attempt, run.status, run.queue_reason, run.failure_reason, run.created_at, run.started_at, run.completed_at, run.updated_at,
			agent.id::text, model.id::text
		FROM scheduler_jobs AS job
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
		WHERE job.state='QUEUED'
		  AND job.available_at <= now()
		  AND run.status='QUEUED'
		ORDER BY job.available_at, job.created_at, job.id
		FOR UPDATE OF job, run SKIP LOCKED
		LIMIT 1
	`).Scan(
		&job.ID, &job.ProjectID, &job.RunID, &job.Kind, &job.State, &job.WaitReason, &job.IdempotencyKey, &job.AvailableAt, &job.CreatedAt, &job.UpdatedAt,
		&run.ID, &run.ProjectID, &run.IssueID, &run.WorkspaceID, &run.AgentID, &run.Attempt, &run.Status, &run.QueueReason, &run.FailureReason, &run.CreatedAt, &run.StartedAt, &run.CompletedAt, &run.UpdatedAt,
		&agentID, &modelProfileID,
	)
	if err != nil {
		return store.SchedulerJob{}, store.Run{}, "", "", notFound(err)
	}
	if agentID == nil || modelProfileID == nil {
		return job, run, "", "", errSchedulerConfigurationUnavailable
	}
	return job, run, *agentID, *modelProfileID, nil
}

func lockAdmissionResources(ctx context.Context, tx pgx.Tx, agentID, modelProfileID string) (int, *int, error) {
	var agentLimit int
	if err := tx.QueryRow(ctx, `SELECT concurrency_limit FROM agents WHERE id=$1 FOR UPDATE`, agentID).Scan(&agentLimit); err != nil {
		return 0, nil, notFound(err)
	}
	var modelLimit *int
	if err := tx.QueryRow(ctx, `SELECT max_concurrent FROM model_profiles WHERE id=$1 FOR UPDATE`, modelProfileID).Scan(&modelLimit); err != nil {
		return 0, nil, notFound(err)
	}
	return agentLimit, modelLimit, nil
}

func countCapacityReservations(ctx context.Context, tx pgx.Tx, resourceKind, resourceID string) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM scheduler_capacity_reservations
		WHERE resource_kind=$1 AND resource_id=$2
	`, resourceKind, resourceID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func deferQueuedJob(ctx context.Context, tx pgx.Tx, job store.SchedulerJob, reason string, backoffMicros int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE scheduler_jobs
		SET wait_reason=$2, available_at=now() + ($3::bigint * interval '1 microsecond'), updated_at=now()
		WHERE id=$1 AND state='QUEUED'
	`, job.ID, reason, backoffMicros); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE runs
		SET queue_reason=$3, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='QUEUED'
	`, job.ProjectID, job.RunID, reason)
	return err
}

func insertCapacityReservation(ctx context.Context, tx pgx.Tx, job store.SchedulerJob, run store.Run, resourceKind, resourceID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO scheduler_capacity_reservations (project_id, job_id, run_id, resource_kind, resource_id)
		VALUES ($1, $2, $3, $4, $5)
	`, job.ProjectID, job.ID, run.ID, resourceKind, resourceID)
	return err
}

// ClaimNextJob is retained for low-level persistence compatibility. Scheduler
// orchestration must use AdmitNextJob so claim and capacity are atomic.
func (s *Store) ClaimNextJob(ctx context.Context, ownerID string, leaseDuration time.Duration) (*store.SchedulerJob, *store.SchedulerLease, error) {
	leaseMicros := leaseDuration.Microseconds()
	if strings.TrimSpace(ownerID) == "" || leaseMicros <= 0 {
		return nil, nil, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var job store.SchedulerJob
	var agentID, modelProfileID *string
	err = tx.QueryRow(ctx, `
		SELECT
			job.id::text, job.project_id::text, job.run_id::text, job.kind, job.state, job.wait_reason, job.idempotency_key, job.available_at, job.created_at, job.updated_at,
			agent.id::text, model.id::text
		FROM scheduler_jobs AS job
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
		WHERE job.state='QUEUED' AND job.available_at <= now()
		ORDER BY job.available_at, job.created_at, job.id
		FOR UPDATE OF job, run SKIP LOCKED
		LIMIT 1
	`).Scan(
		&job.ID, &job.ProjectID, &job.RunID, &job.Kind, &job.State, &job.WaitReason, &job.IdempotencyKey, &job.AvailableAt, &job.CreatedAt, &job.UpdatedAt,
		&agentID, &modelProfileID,
	)
	if errors.Is(notFound(err), store.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if agentID == nil || modelProfileID == nil {
		if err := deferQueuedJob(ctx, tx, job, schedulerConfigurationWaitReason, schedulerConfigurationBackoff.Microseconds()); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}

	lease, err := scanSchedulerLease(tx.QueryRow(ctx, `
		INSERT INTO scheduler_leases (job_id, owner_id, expires_at)
		VALUES ($1, $2, now() + ($3::bigint * interval '1 microsecond'))
		RETURNING job_id::text, owner_id, lease_token::text, acquired_at, expires_at
	`, job.ID, ownerID, leaseMicros))
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE scheduler_jobs SET state = 'CLAIMED', updated_at = now() WHERE id = $1`, job.ID); err != nil {
		return nil, nil, err
	}
	job.State = "CLAIMED"
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return &job, &lease, nil
}

func (s *Store) RenewLease(ctx context.Context, projectID, jobID, leaseToken string, leaseDuration time.Duration) (store.SchedulerLease, error) {
	leaseMicros := leaseDuration.Microseconds()
	if leaseMicros <= 0 {
		return store.SchedulerLease{}, store.ErrInvalidArgument
	}
	return scanSchedulerLease(s.pool.QueryRow(ctx, `
		UPDATE scheduler_leases AS lease
		SET expires_at = now() + ($4::bigint * interval '1 microsecond')
		FROM scheduler_jobs AS job
		WHERE lease.job_id = $2
		  AND lease.lease_token = $3
		  AND job.id = lease.job_id
		  AND job.project_id = $1
		  AND lease.expires_at > now()
		RETURNING lease.job_id::text, lease.owner_id, lease.lease_token::text, lease.acquired_at, lease.expires_at
	`, projectID, jobID, leaseToken, leaseMicros))
}

func (s *Store) ReleaseLease(ctx context.Context, projectID, jobID, leaseToken string) error {
	command, err := s.pool.Exec(ctx, `
		DELETE FROM scheduler_leases AS lease
		USING scheduler_jobs AS job
		WHERE lease.job_id = $2
		  AND lease.lease_token = $3
		  AND job.id = lease.job_id
		  AND job.project_id = $1
	`, projectID, jobID, leaseToken)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

// ReserveCapacity is retained for low-level persistence compatibility. Scheduler
// orchestration must use AdmitNextJob so capacity checks and reservations are atomic.
func (s *Store) ReserveCapacity(ctx context.Context, projectID, jobID, runID, resourceKind, resourceID string) error {
	if resourceKind != "AGENT" && resourceKind != "MODEL_PROFILE" {
		return store.ErrInvalidArgument
	}

	command, err := s.pool.Exec(ctx, `
		INSERT INTO scheduler_capacity_reservations (project_id, job_id, run_id, resource_kind, resource_id)
		SELECT $1, job.id, job.run_id, $4, $5
		FROM scheduler_jobs AS job
		WHERE job.id = $2
		  AND job.project_id = $1
		  AND job.run_id = $3
		  AND (
			($4 = 'AGENT' AND EXISTS (
				SELECT 1 FROM agents AS agent
				WHERE agent.id = $5 AND (agent.project_id IS NULL OR agent.project_id = $1)
			))
			OR
			($4 = 'MODEL_PROFILE' AND EXISTS (
				SELECT 1 FROM model_profiles AS model_profile
				WHERE model_profile.id = $5 AND (model_profile.project_id IS NULL OR model_profile.project_id = $1)
			))
		  )
		ON CONFLICT (job_id, resource_kind) DO NOTHING
	`, projectID, jobID, runID, resourceKind, resourceID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		var exists bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM scheduler_capacity_reservations
				WHERE project_id = $1 AND job_id = $2 AND run_id = $3 AND resource_kind = $4 AND resource_id = $5
			)
		`, projectID, jobID, runID, resourceKind, resourceID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return store.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReleaseCapacity(ctx context.Context, projectID, jobID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM scheduler_capacity_reservations WHERE project_id = $1 AND job_id = $2`, projectID, jobID)
	return err
}

func (s *Store) TransitionAdmittedJob(ctx context.Context, input store.SchedulerTransition) (store.Run, error) {
	return s.transitionAdmittedJob(ctx, input)
}

func (s *Store) ClaimExpiredJobForReconciliation(ctx context.Context, ownerID string, leaseDuration time.Duration) (*store.SchedulerAdmission, error) {
	return s.claimExpiredJobForReconciliation(ctx, ownerID, leaseDuration)
}

func (s *Store) ResolveReconciliation(ctx context.Context, input store.SchedulerReconciliation) (store.Run, error) {
	return s.resolveReconciliation(ctx, input)
}

func scanSchedulerJob(row pgx.Row) (store.SchedulerJob, error) {
	var value store.SchedulerJob
	if err := row.Scan(&value.ID, &value.ProjectID, &value.RunID, &value.Kind, &value.State, &value.WaitReason, &value.IdempotencyKey, &value.AvailableAt, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.SchedulerJob{}, notFound(err)
	}
	return value, nil
}

func scanSchedulerLease(row pgx.Row) (store.SchedulerLease, error) {
	var value store.SchedulerLease
	if err := row.Scan(&value.JobID, &value.OwnerID, &value.LeaseToken, &value.AcquiredAt, &value.ExpiresAt); err != nil {
		return store.SchedulerLease{}, notFound(err)
	}
	return value, nil
}
