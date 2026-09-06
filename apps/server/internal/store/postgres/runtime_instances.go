package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

var runtimeInstanceStatuses = map[string]struct{}{
	"PROVISIONING": {},
	"STARTING":     {},
	"RUNNING":      {},
	"STOPPING":     {},
	"FAILED":       {},
	"STOPPED":      {},
	"DESTROYED":    {},
}

var runnerStatuses = map[string]struct{}{
	"CONNECTING":  {},
	"READY":       {},
	"BUSY":        {},
	"DRAINING":    {},
	"UNAVAILABLE": {},
}

func (s *Store) GetRuntimeInstance(ctx context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(instanceID) == "" {
		return store.RuntimeInstance{}, store.ErrInvalidArgument
	}
	return scanRuntimeInstance(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, workspace_id::text, runtime_id::text, status, external_id, runner_status, safe_handle_metadata, created_at, started_at, stopped_at, updated_at
		FROM runtime_instances
		WHERE project_id = $1 AND id = $2
	`, projectID, instanceID))
}

func (s *Store) ListRuntimeInstances(ctx context.Context, projectID string, statuses []string) ([]store.RuntimeInstance, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, store.ErrInvalidArgument
	}
	for _, status := range statuses {
		if _, ok := runtimeInstanceStatuses[status]; !ok {
			return nil, store.ErrInvalidArgument
		}
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id::text, project_id::text, workspace_id::text, runtime_id::text, status, external_id, runner_status, safe_handle_metadata, created_at, started_at, stopped_at, updated_at
		FROM runtime_instances
		WHERE project_id = $1
		  AND (coalesce(cardinality($2::text[]), 0) = 0 OR status = ANY($2::text[]))
		ORDER BY created_at, id
	`, projectID, statuses)
	if err != nil {
		return nil, notFound(err)
	}
	defer rows.Close()

	instances := make([]store.RuntimeInstance, 0)
	for rows.Next() {
		instance, err := scanRuntimeInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return instances, nil
}

func (s *Store) UpdateRuntimeInstanceRunnerStatus(ctx context.Context, projectID, instanceID, runnerStatus string) (store.RuntimeInstance, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(instanceID) == "" {
		return store.RuntimeInstance{}, store.ErrInvalidArgument
	}
	if _, ok := runnerStatuses[runnerStatus]; !ok {
		return store.RuntimeInstance{}, store.ErrInvalidArgument
	}
	return scanRuntimeInstance(s.pool.QueryRow(ctx, `
		UPDATE runtime_instances
		SET runner_status = $3, updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING id::text, project_id::text, workspace_id::text, runtime_id::text, status, external_id, runner_status, safe_handle_metadata, created_at, started_at, stopped_at, updated_at
	`, projectID, instanceID, runnerStatus))
}

// ClaimRuntimeInstanceRunnerGeneration atomically supersedes any previous
// server-side runner connection for a RUNNING Runtime Instance. The returned
// generation is the ownership token for connection-scoped status reports.
func (s *Store) ClaimRuntimeInstanceRunnerGeneration(ctx context.Context, projectID, instanceID string) (int64, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(instanceID) == "" {
		return 0, store.ErrInvalidArgument
	}
	var generation int64
	err := s.pool.QueryRow(ctx, `
		UPDATE runtime_instances
		SET runner_generation = runner_generation + 1,
		    runner_status = 'CONNECTING',
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND status = 'RUNNING'
		RETURNING runner_generation
	`, projectID, instanceID).Scan(&generation)
	if err == nil {
		return generation, nil
	}
	return 0, s.runnerGenerationMiss(ctx, projectID, instanceID, notFound(err))
}

// UpdateRuntimeInstanceRunnerStatusGeneration persists connection-owned runner
// state only while generation is still the current durable ownership token.
func (s *Store) UpdateRuntimeInstanceRunnerStatusGeneration(ctx context.Context, projectID, instanceID, runnerStatus string, generation int64) (store.RuntimeInstance, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(instanceID) == "" || generation < 1 {
		return store.RuntimeInstance{}, store.ErrInvalidArgument
	}
	if _, ok := runnerStatuses[runnerStatus]; !ok {
		return store.RuntimeInstance{}, store.ErrInvalidArgument
	}
	instance, err := scanRuntimeInstance(s.pool.QueryRow(ctx, `
		UPDATE runtime_instances
		SET runner_status = $3, updated_at = now()
		WHERE project_id = $1 AND id = $2 AND runner_generation = $4
		RETURNING id::text, project_id::text, workspace_id::text, runtime_id::text, status, external_id, runner_status, safe_handle_metadata, created_at, started_at, stopped_at, updated_at
	`, projectID, instanceID, runnerStatus, generation))
	if err == nil {
		return instance, nil
	}
	return store.RuntimeInstance{}, s.runnerGenerationMiss(ctx, projectID, instanceID, err)
}

func (s *Store) runnerGenerationMiss(ctx context.Context, projectID, instanceID string, updateErr error) error {
	if !errors.Is(updateErr, store.ErrNotFound) {
		return updateErr
	}
	if _, err := s.GetRuntimeInstance(ctx, projectID, instanceID); err == nil {
		return store.ErrConflict
	} else {
		return err
	}
}
