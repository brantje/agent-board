package postgres

import (
	"context"
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
