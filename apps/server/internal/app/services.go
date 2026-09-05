package app

import (
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// Services groups the control-plane API service with the execution-facing
// Workspace service so server startup constructs one coherent application
// boundary. Future scheduler workers use Workspaces before Runtime provisioning.
type Services struct {
	ControlPlane *Service
	Workspaces    *WorkspaceService
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
