package postgres

import (
	"context"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

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
