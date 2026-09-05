package app

import (
	"fmt"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

// Services groups the control-plane API with execution-facing services so
// server startup constructs one coherent application boundary.
type Services struct {
	ControlPlane     *Service
	Workspaces       *WorkspaceService
	RuntimeInstances *RuntimeInstanceService
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
	services.RuntimeInstances = runtimeInstances
	return services, nil
}

func (s *Services) Close() error {
	if s == nil || s.RuntimeInstances == nil {
		return nil
	}
	return s.RuntimeInstances.Close()
}
