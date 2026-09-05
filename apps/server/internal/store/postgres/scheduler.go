package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) EnqueueJob(ctx context.Context, input store.SchedulerJob) (store.SchedulerJob, error) {
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
		VALUES ($1, $2, $3, 'QUEUED', $4, $5, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id::text, project_id::text, run_id::text, kind, state, wait_reason, idempotency_key, available_at, created_at, updated_at
	`, input.ProjectID, input.RunID, kind, input.WaitReason, input.IdempotencyKey, availableAt))
	if err == nil {
		return job, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.SchedulerJob{}, err
	}
	return scanSchedulerJob(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, run_id::text, kind, state, wait_reason, idempotency_key, available_at, created_at, updated_at
		FROM scheduler_jobs
		WHERE idempotency_key = $1 AND project_id = $2 AND run_id = $3 AND kind = $4
	`, input.IdempotencyKey, input.ProjectID, input.RunID, kind))
}

func (s *Store) ClaimNextJob(ctx context.Context, ownerID string, leaseDuration time.Duration) (*store.SchedulerJob, *store.SchedulerLease, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	job, err := scanSchedulerJob(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, run_id::text, kind, state, wait_reason, idempotency_key, available_at, created_at, updated_at
		FROM scheduler_jobs
		WHERE state = 'QUEUED' AND available_at <= now()
		ORDER BY available_at, created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	expiresAt := time.Now().Add(leaseDuration)
	lease, err := scanSchedulerLease(tx.QueryRow(ctx, `
		INSERT INTO scheduler_leases (job_id, owner_id, expires_at)
		VALUES ($1, $2, $3)
		RETURNING job_id::text, owner_id, lease_token::text, acquired_at, expires_at
	`, job.ID, ownerID, expiresAt))
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

func (s *Store) RenewLease(ctx context.Context, jobID, leaseToken string, leaseDuration time.Duration) (store.SchedulerLease, error) {
	return scanSchedulerLease(s.pool.QueryRow(ctx, `
		UPDATE scheduler_leases
		SET expires_at = now() + $3::interval
		WHERE job_id = $1 AND lease_token = $2
		RETURNING job_id::text, owner_id, lease_token::text, acquired_at, expires_at
	`, jobID, leaseToken, leaseDuration.String()))
}

func (s *Store) ReleaseLease(ctx context.Context, jobID, leaseToken string) error {
	command, err := s.pool.Exec(ctx, `DELETE FROM scheduler_leases WHERE job_id = $1 AND lease_token = $2`, jobID, leaseToken)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ReserveCapacity(ctx context.Context, projectID, jobID, runID, resourceKind, resourceID string) error {
	command, err := s.pool.Exec(ctx, `
		INSERT INTO scheduler_capacity_reservations (project_id, job_id, run_id, resource_kind, resource_id)
		SELECT $1, id, run_id, $4, $5
		FROM scheduler_jobs
		WHERE id = $2 AND project_id = $1 AND run_id = $3
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

func (s *Store) ReleaseCapacity(ctx context.Context, jobID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM scheduler_capacity_reservations WHERE job_id = $1`, jobID)
	return err
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
