package app

import (
	"context"
	"fmt"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
)

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
