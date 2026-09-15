package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const schedulerConfigurationWaitReason = "configuration_unavailable"

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
		JOIN model_profiles AS model
		  ON model.id=agent.model_profile_id
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

	occupied, err := workspaceAdmissionOccupied(ctx, tx, run)
	if err != nil {
		return nil, err
	}
	if occupied {
		if err := deferQueuedJob(ctx, tx, job, store.SchedulerWaitWorkspace, backoffMicros); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
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

	runnerID, err := s.lockRunnerCandidate(ctx, tx, run.ProjectID, agentID)
	if errors.Is(err, store.ErrNotFound) {
		if err := deferQueuedJob(ctx, tx, job, "runner_capacity", backoffMicros); err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	}
	if err != nil {
		return nil, err
	}
	if err := insertCapacityReservation(ctx, tx, job, run, "RUNNER", runnerID); err != nil {
		return nil, err
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
		RunnerID:       runnerID,
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
		JOIN issues AS issue ON issue.project_id=run.project_id AND issue.id=run.issue_id
		JOIN projects AS project ON project.id=job.project_id
		LEFT JOIN agents AS agent
		  ON agent.id=run.agent_id
		 AND (agent.project_id IS NULL OR agent.project_id=run.project_id)
		LEFT JOIN model_profiles AS model
		  ON model.id=agent.model_profile_id
		 AND (model.project_id IS NULL OR model.project_id=run.project_id)
		WHERE job.state='QUEUED'
		  AND job.available_at <= now()
		  AND run.status='QUEUED'
		  AND (
			COALESCE((project.workflow_settings->>'strictOrder')::boolean, false) = false
			OR (
			  NOT EXISTS (
				SELECT 1
				FROM issue_relationships AS rel
				JOIN issues AS dependency
				  ON dependency.project_id=rel.project_id
				 AND dependency.id=CASE WHEN rel.type='depends_on' THEN rel.target_issue_id ELSE rel.source_issue_id END
				WHERE rel.project_id=issue.project_id
				  AND ((rel.type='depends_on' AND rel.source_issue_id=issue.id) OR (rel.type='blocks' AND rel.target_issue_id=issue.id))
				  AND dependency.status<>'DONE'
			  )
			  AND NOT EXISTS (
				SELECT 1 FROM runs AS peer
				WHERE peer.project_id=run.project_id AND peer.issue_id=run.issue_id AND peer.id<>run.id
				  AND peer.status IN ('STARTING','RUNNING','WAITING_FOR_INPUT','PAUSED','READY_FOR_REVIEW')
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM scheduler_jobs AS prior_job
				JOIN runs AS prior_run ON prior_run.project_id=prior_job.project_id AND prior_run.id=prior_job.run_id
				JOIN issues AS prior_issue ON prior_issue.project_id=prior_run.project_id AND prior_issue.id=prior_run.issue_id
				JOIN agents AS prior_agent ON prior_agent.id=prior_run.agent_id AND (prior_agent.project_id IS NULL OR prior_agent.project_id=prior_run.project_id)
				JOIN model_profiles AS prior_model ON prior_model.id=prior_agent.model_profile_id AND (prior_model.project_id IS NULL OR prior_model.project_id=prior_run.project_id)
				WHERE prior_job.project_id=job.project_id
				  AND prior_job.state='QUEUED' AND prior_job.available_at<=now() AND prior_run.status='QUEUED'
				  AND NOT EXISTS (
					SELECT 1
					FROM issue_relationships AS prior_rel
					JOIN issues AS prior_dependency
					  ON prior_dependency.project_id=prior_rel.project_id
					 AND prior_dependency.id=CASE WHEN prior_rel.type='depends_on' THEN prior_rel.target_issue_id ELSE prior_rel.source_issue_id END
					WHERE prior_rel.project_id=prior_issue.project_id
					  AND ((prior_rel.type='depends_on' AND prior_rel.source_issue_id=prior_issue.id) OR (prior_rel.type='blocks' AND prior_rel.target_issue_id=prior_issue.id))
					  AND prior_dependency.status<>'DONE'
				  )
				  AND NOT EXISTS (
					SELECT 1 FROM runs AS prior_peer
					WHERE prior_peer.project_id=prior_run.project_id AND prior_peer.issue_id=prior_run.issue_id AND prior_peer.id<>prior_run.id
					  AND prior_peer.status IN ('STARTING','RUNNING','WAITING_FOR_INPUT','PAUSED','READY_FOR_REVIEW')
				  )
				  AND ROW(
					COALESCE(array_position(ARRAY['TODO','IN_PROGRESS','BLOCKED','REVIEW']::text[], prior_issue.status), 99),
					prior_issue.board_position, prior_job.created_at, prior_job.id
				  ) < ROW(
					COALESCE(array_position(ARRAY['TODO','IN_PROGRESS','BLOCKED','REVIEW']::text[], issue.status), 99),
					issue.board_position, job.created_at, job.id
				  )
			  )
			)
		  )
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

// workspaceAdmissionOccupied serializes admission for Runs that share the
// durable Issue Workspace. A peer claim covers the short pre-session window; a
// live Execution Session keeps ownership while native Question input is pending.
// Capacity is intentionally checked only after this fence so waiting work holds
// no Agent, Model Profile or Runner reservation.
func workspaceAdmissionOccupied(ctx context.Context, tx pgx.Tx, run store.Run) (bool, error) {
	var workspaceID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM workspaces
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, run.ProjectID, run.WorkspaceID).Scan(&workspaceID); err != nil {
		return false, notFound(err)
	}

	var occupied bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM runs AS peer
			WHERE peer.project_id=$1
			  AND peer.workspace_id=$2
			  AND peer.id<>$3
			  AND (
				EXISTS (
					SELECT 1 FROM scheduler_jobs AS claimed
					WHERE claimed.project_id=peer.project_id
					  AND claimed.run_id=peer.id
					  AND claimed.state='CLAIMED'
				)
				OR EXISTS (
					SELECT 1 FROM execution_sessions AS session
					WHERE session.project_id=peer.project_id
					  AND session.run_id=peer.id
					  AND session.status IN ('PENDING','STARTING','RUNNING')
				)
			  )
		)
	`, run.ProjectID, workspaceID, run.ID).Scan(&occupied); err != nil {
		return false, err
	}
	return occupied, nil
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

func (s *Store) RenewLease(ctx context.Context, projectID, jobID, leaseToken string, leaseDuration time.Duration) (store.SchedulerLease, error) {
	leaseMicros := leaseDuration.Microseconds()
	if leaseMicros <= 0 {
		return store.SchedulerLease{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.SchedulerLease{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var runID string
	if err := tx.QueryRow(ctx, `
		SELECT run.id::text
		FROM scheduler_jobs AS job
		JOIN scheduler_leases AS lease ON lease.job_id=job.id
		JOIN runs AS run ON run.project_id=job.project_id AND run.id=job.run_id
		WHERE job.project_id=$1
		  AND job.id=$2
		  AND lease.lease_token=$3
		  AND lease.expires_at > now()
		FOR UPDATE OF run
	`, projectID, jobID, leaseToken).Scan(&runID); err != nil {
		return store.SchedulerLease{}, notFound(err)
	}

	lease, err := scanSchedulerLease(tx.QueryRow(ctx, `
		UPDATE scheduler_leases AS lease
		SET expires_at = now() + ($4::bigint * interval '1 microsecond')
		FROM scheduler_jobs AS job
		WHERE lease.job_id = $2
		  AND lease.lease_token = $3
		  AND job.id = lease.job_id
		  AND job.project_id = $1
		  AND job.run_id = $5
		  AND lease.expires_at > now()
		RETURNING lease.job_id::text, lease.owner_id, lease.lease_token::text, lease.acquired_at, lease.expires_at
	`, projectID, jobID, leaseToken, leaseMicros, runID))
	if err != nil {
		return store.SchedulerLease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.SchedulerLease{}, err
	}
	return lease, nil
}
