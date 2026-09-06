package app

import (
	"errors"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

// Services groups the control-plane API with execution-facing services so
// server startup constructs one coherent application boundary.
type Services struct {
	ControlPlane      *Service
	Workspaces        *WorkspaceService
	RuntimeInstances  *RuntimeInstanceService
	RunnerConnections *runner.Manager
	ExecutionSessions *ExecutionSessionService
}

func NewServices(controlPlaneStore store.ControlPlaneStore, materializer WorkspaceMaterializer) (*Services, error) {
	if controlPlaneStore == nil {
		return nil, fmt.Errorf("control-plane store is required")
	}
	controlPlane := New(controlPlaneStore)
	workspaces, err := NewWorkspaceService(controlPlaneStore, materializer)
	if err != nil {
		return nil, err
	}
	return &Services{ControlPlane: controlPlane, Workspaces: workspaces}, nil
}

func NewServicesWithRuntimes(controlPlaneStore store.ControlPlaneStore, materializer WorkspaceMaterializer, implementations map[string]runtimepkg.Implementation) (*Services, error) {
	services, err := NewServices(controlPlaneStore, materializer)
	if err != nil {
		return nil, err
	}
	runtimeInstances, err := NewRuntimeInstanceService(controlPlaneStore, services.Workspaces, implementations)
	if err != nil {
		return nil, err
	}
	runnerConnections, err := runner.NewManager(runtimeInstances, runtimeInstances)
	if err != nil {
		_ = runtimeInstances.Close()
		return nil, err
	}
	executionSessions, err := NewExecutionSessionService(controlPlaneStore, runnerConnections)
	if err != nil {
		_ = runnerConnections.Close()
		_ = runtimeInstances.Close()
		return nil, err
	}
	services.RuntimeInstances = runtimeInstances
	services.RunnerConnections = runnerConnections
	services.ExecutionSessions = executionSessions
	return services, nil
}

func (s *Services) Close() error {
	if s == nil {
		return nil
	}
	var closeErrors []error
	// Close transport first so no runner operation can race Runtime teardown.
	if s.RunnerConnections != nil {
		closeErrors = append(closeErrors, s.RunnerConnections.Close())
	}
	if s.RuntimeInstances != nil {
		closeErrors = append(closeErrors, s.RuntimeInstances.Close())
	}
	return errors.Join(closeErrors...)
}
