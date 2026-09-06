package app

import (
	"context"
	"fmt"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runnerStatusStore interface {
	UpdateRuntimeInstanceRunnerStatus(context.Context, string, string, string) (store.RuntimeInstance, error)
}

type runnerGenerationStore interface {
	ClaimRuntimeInstanceRunnerGeneration(context.Context, string, string) (int64, error)
	UpdateRuntimeInstanceRunnerStatusGeneration(context.Context, string, string, string, int64) (store.RuntimeInstance, error)
}

// RunnerEndpoint resolves the execution-plane endpoint for a persisted Runtime
// Instance while keeping implementation-specific handles inside the Runtime
// boundary.
func (s *RuntimeInstanceService) RunnerEndpoint(ctx context.Context, projectID, instanceID string) (runtimepkg.RunnerEndpoint, error) {
	instance, implementation, handle, err := s.loadInstance(ctx, projectID, instanceID)
	if err != nil {
		return runtimepkg.RunnerEndpoint{}, err
	}
	if instance.Status != string(runtimepkg.StateRunning) {
		return runtimepkg.RunnerEndpoint{}, NewError("runtime_instance_not_running", "Runtime Instance is not running", runtimepkg.ErrRunnerUnavailable)
	}
	provider, ok := implementation.(runtimepkg.RunnerEndpointProvider)
	if !ok {
		return runtimepkg.RunnerEndpoint{}, NewError("runner_endpoint_unsupported", "Runtime implementation does not expose a runner endpoint", runtimepkg.ErrUnsupportedPolicy)
	}
	endpoint, err := provider.RunnerEndpoint(ctx, handle)
	if err != nil {
		return runtimepkg.RunnerEndpoint{}, fmt.Errorf("resolve runner endpoint: %w", err)
	}
	return endpoint, nil
}

// SetRunnerStatus persists execution-owned runner availability separately from
// Runtime Instance lifecycle state. It intentionally leaves the connection
// generation untouched.
func (s *RuntimeInstanceService) SetRunnerStatus(ctx context.Context, projectID, instanceID, status string) error {
	statusStore, ok := s.store.(runnerStatusStore)
	if !ok {
		return fmt.Errorf("runtime instance store does not support runner status updates")
	}
	before, err := s.getInstance(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	after, err := statusStore.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, instanceID, status)
	if err != nil {
		return translateStoreError(err, "runtime_instance")
	}
	return validateRunnerStatusBinding(before, after)
}

// ClaimRunnerConnection atomically supersedes any earlier server connection to
// the Runtime Instance runner and returns its durable ownership generation.
func (s *RuntimeInstanceService) ClaimRunnerConnection(ctx context.Context, projectID, instanceID string) (int64, error) {
	generationStore, ok := s.store.(runnerGenerationStore)
	if !ok {
		return 0, fmt.Errorf("runtime instance store does not support runner connection generations")
	}
	instance, err := s.getInstance(ctx, projectID, instanceID)
	if err != nil {
		return 0, err
	}
	if instance.Status != string(runtimepkg.StateRunning) {
		return 0, NewError("runtime_instance_not_running", "Runtime Instance is not running", runtimepkg.ErrRunnerUnavailable)
	}
	generation, err := generationStore.ClaimRuntimeInstanceRunnerGeneration(ctx, projectID, instanceID)
	if err != nil {
		return 0, translateStoreError(err, "runtime_instance")
	}
	return generation, nil
}

// SetRunnerStatusGeneration persists connection-owned runner state only while
// generation still owns the durable runner connection slot.
func (s *RuntimeInstanceService) SetRunnerStatusGeneration(ctx context.Context, projectID, instanceID, status string, generation int64) error {
	generationStore, ok := s.store.(runnerGenerationStore)
	if !ok {
		return fmt.Errorf("runtime instance store does not support runner connection generations")
	}
	before, err := s.getInstance(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	after, err := generationStore.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, projectID, instanceID, status, generation)
	if err != nil {
		return translateStoreError(err, "runtime_instance")
	}
	return validateRunnerStatusBinding(before, after)
}

func validateRunnerStatusBinding(before, after store.RuntimeInstance) error {
	if before.WorkspaceID != after.WorkspaceID || before.RuntimeID != after.RuntimeID || before.Status != after.Status {
		return fmt.Errorf("runtime instance immutable/lifecycle state changed during runner status update")
	}
	return nil
}
