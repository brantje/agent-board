package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
	workspacepkg "github.com/brantje/agent-board/apps/server/internal/workspace"
)

// WorkspaceMaterializer is the application-facing boundary for making one
// Issue's durable Workspace ready for execution. Scheduler/runtime code calls
// this boundary before provisioning compute; HTTP assignment remains limited to
// reserving durable execution intent.
type WorkspaceMaterializer interface {
	Ensure(context.Context, store.Project, store.Issue, store.Workspace) (store.Workspace, error)
}

type WorkspaceLookupStore interface {
	GetProject(context.Context, string) (store.Project, error)
	GetIssue(context.Context, string, string) (store.Issue, error)
	GetWorkspaceByIssue(context.Context, string, string) (store.Workspace, error)
}

type WorkspaceService struct {
	store        WorkspaceLookupStore
	materializer WorkspaceMaterializer
}

func NewWorkspaceService(workspaceStore WorkspaceLookupStore, materializer WorkspaceMaterializer) (*WorkspaceService, error) {
	if workspaceStore == nil || materializer == nil {
		return nil, fmt.Errorf("workspace service dependencies are required")
	}
	return &WorkspaceService{store: workspaceStore, materializer: materializer}, nil
}

// EnsureIssueWorkspace materializes or reuses the single authoritative
// Workspace reserved for an Issue. It deliberately does not create a second
// Workspace identity: assignment owns that durable reservation, while this
// method makes the reserved checkout usable before execution.
func (s *WorkspaceService) EnsureIssueWorkspace(ctx context.Context, projectID, issueID string) (store.Workspace, error) {
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return store.Workspace{}, translateStoreError(err, "project")
	}
	issue, err := s.store.GetIssue(ctx, projectID, issueID)
	if err != nil {
		return store.Workspace{}, translateStoreError(err, "issue")
	}
	current, err := s.store.GetWorkspaceByIssue(ctx, projectID, issueID)
	if err != nil {
		return store.Workspace{}, translateStoreError(err, "workspace")
	}

	ready, err := s.materializer.Ensure(ctx, project, issue, current)
	if err == nil {
		return ready, nil
	}
	switch {
	case errors.Is(err, workspacepkg.ErrBootstrapFailed):
		return store.Workspace{}, NewError("workspace_bootstrap_failed", "workspace repository bootstrap failed", err)
	case errors.Is(err, workspacepkg.ErrInvalidMetadata), errors.Is(err, workspacepkg.ErrInvalidRoot):
		return store.Workspace{}, NewError("workspace_configuration_invalid", "workspace configuration is invalid", err)
	default:
		return store.Workspace{}, err
	}
}
