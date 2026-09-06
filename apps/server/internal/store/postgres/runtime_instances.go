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
	if err := validateRunnerStatusUpdate(projectID, instanceID, runnerStatus); err != nil {
		return store.RuntimeInstance{}, err
	}
	return scanRuntimeInstance(s.pool.QueryRow(ctx, `
		UPDATE runtime_instances
		SET runner_status = $3, updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING id::text, project_id::text, workspace_id::text, runtime_id::text, status, external_id, runner_status, safe_handle_metadata, created_at, started_at, stopped_at, updated_at
	`, projectID, instanceID, runnerStatus))
}

// UpdateRuntimeInstanceRunnerStatusIfStatus persists a runner-status mutation
// only while the Runtime Instance lifecycle status still matches the caller's
// observed status. A lifecycle race is returned as ErrConflict.
func (s *Store) UpdateRuntimeInstanceRunnerStatusIfStatus(ctx context.Context, projectID, instanceID, runnerStatus, expectedStatus string) (store.RuntimeInstance, error) {
	if err := validateRunnerStatusUpdate(projectID, instanceID, runnerStatus); err != nil {
		return store.RuntimeInstance{}, err
	}
	if _, ok := runtimeInstanceStatuses[expectedStatus]; !ok {
		return store.RuntimeInstance{}, store.ErrInvalidArgument
	}
	instance, err := scanRuntimeInstance(s.pool.QueryRow(ctx, `
		UPDATE runtime_instances
		SET runner_status = $3, updated_at = now()
		WHERE project_id = $1 AND id = $2 AND status = $4
		RETURNING id::text, project_id::text, workspace_id::text, runtime_id::text, status, external_id, runner_status, safe_handle_metadata, created_at, started_at, stopped_at, updated_at
	`, projectID, instanceID, runnerStatus, expectedStatus))
	if err == nil {
		return instance, nil
	}
	return store.RuntimeInstance{}, s.runtimeInstanceUpdateMiss(ctx, projectID, instanceID, err)
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
	if err := validateRunnerGenerationUpdate(projectID, instanceID, runnerStatus, generation); err != nil {
		return store.RuntimeInstance{}, err
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

// UpdateRuntimeInstanceRunnerStatusGenerationIfStatus fences a connection-owned
// runner-status mutation by both the runner generation and the lifecycle state
// observed by the caller. Either ownership loss or lifecycle movement is a
// conflict and cannot mutate durable runner state.
func (s *Store) UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(ctx context.Context, projectID, instanceID, runnerStatus string, generation int64, expectedStatus string) (store.RuntimeInstance, error) {
	if err := validateRunnerGenerationUpdate(projectID, instanceID, runnerStatus, generation); err != nil {
		return store.RuntimeInstance{}, err
	}
	if _, ok := runtimeInstanceStatuses[expectedStatus]; !ok {
		return store.RuntimeInstance{}, store.ErrInvalidArgument
	}
	instance, err := scanRuntimeInstance(s.pool.QueryRow(ctx, `
		UPDATE runtime_instances
		SET runner_status = $3, updated_at = now()
		WHERE project_id = $1 AND id = $2 AND runner_generation = $4 AND status = $5
		RETURNING id::text, project_id::text, workspace_id::text, runtime_id::text, status, external_id, runner_status, safe_handle_metadata, created_at, started_at, stopped_at, updated_at
	`, projectID, instanceID, runnerStatus, generation, expectedStatus))
	if err == nil {
		return instance, nil
	}
	return store.RuntimeInstance{}, s.runtimeInstanceUpdateMiss(ctx, projectID, instanceID, err)
}

func validateRunnerStatusUpdate(projectID, instanceID, runnerStatus string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(instanceID) == "" {
		return store.ErrInvalidArgument
	}
	if _, ok := runnerStatuses[runnerStatus]; !ok {
		return store.ErrInvalidArgument
	}
	return nil
}

func validateRunnerGenerationUpdate(projectID, instanceID, runnerStatus string, generation int64) error {
	if generation < 1 {
		return store.ErrInvalidArgument
	}
	return validateRunnerStatusUpdate(projectID, instanceID, runnerStatus)
}

func (s *Store) runnerGenerationMiss(ctx context.Context, projectID, instanceID string, updateErr error) error {
	return s.runtimeInstanceUpdateMiss(ctx, projectID, instanceID, updateErr)
}

func (s *Store) runtimeInstanceUpdateMiss(ctx context.Context, projectID, instanceID string, updateErr error) error {
	if !errors.Is(updateErr, store.ErrNotFound) {
		return updateErr
	}
	if _, err := s.GetRuntimeInstance(ctx, projectID, instanceID); err == nil {
		return store.ErrConflict
	} else {
		return err
	}
}
