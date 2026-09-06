package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// UpdateRuntimeInstanceRunnerStatusIfStatus lets the existing runner-status
// coverage fake exercise the lifecycle-fenced RuntimeInstanceService path while
// preserving its configured write errors and returned-binding mutations.
func (s *runnerStatusRuntimeStore) UpdateRuntimeInstanceRunnerStatusIfStatus(ctx context.Context, projectID, instanceID, status, expectedStatus string) (store.RuntimeInstance, error) {
	if s.instance.Status != expectedStatus {
		return store.RuntimeInstance{}, store.ErrConflict
	}
	return s.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, instanceID, status)
}
