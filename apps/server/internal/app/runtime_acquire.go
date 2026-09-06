package app

import (
	"context"
	"errors"
	"strings"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runtimeInstanceLister interface {
	ListRuntimeInstances(context.Context, string, []string) ([]store.RuntimeInstance, error)
}

// Acquire returns reusable compute for the Issue Workspace when possible.
func (s *RuntimeInstanceService) Acquire(ctx context.Context, projectID, issueID, runtimeID string) (store.RuntimeInstance, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || strings.TrimSpace(runtimeID) == "" {
		return store.RuntimeInstance{}, NewError("invalid_argument", "projectId, issueId and runtimeId are required", store.ErrInvalidArgument)
	}
	workspace, err := s.workspaces.EnsureIssueWorkspace(ctx, projectID, issueID)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	if workspace.ProjectID != projectID || workspace.IssueID != issueID || workspace.BootstrapStatus != "READY" {
		return store.RuntimeInstance{}, NewError("workspace_not_ready", "workspace is not ready for Runtime provisioning", store.ErrInvalidArgument)
	}
	if _, _, err := s.resolveImplementation(ctx, projectID, runtimeID); err != nil {
		return store.RuntimeInstance{}, err
	}

	lister, ok := s.store.(runtimeInstanceLister)
	if !ok {
		return s.Create(ctx, projectID, issueID, runtimeID)
	}
	instances, err := lister.ListRuntimeInstances(ctx, projectID, []string{string(runtimepkg.StateRunning), string(runtimepkg.StateStopped)})
	if err != nil {
		return store.RuntimeInstance{}, translateStoreError(err, "runtime_instance")
	}

	for i := len(instances) - 1; i >= 0; i-- {
		instance := instances[i]
		if instance.WorkspaceID != workspace.ID || instance.RuntimeID != runtimeID || instance.Status != string(runtimepkg.StateRunning) {
			continue
		}
		inspection, err := s.Inspect(ctx, projectID, instance.ID)
		if err == nil && inspection.State == runtimepkg.StateRunning {
			return instance, nil
		}
		if err != nil && !errors.Is(err, runtimepkg.ErrNotFound) {
			return store.RuntimeInstance{}, err
		}
	}
	for i := len(instances) - 1; i >= 0; i-- {
		instance := instances[i]
		if instance.WorkspaceID != workspace.ID || instance.RuntimeID != runtimeID || instance.Status != string(runtimepkg.StateStopped) {
			continue
		}
		started, err := s.Start(ctx, projectID, instance.ID)
		if err == nil {
			return started, nil
		}
		if !errors.Is(err, runtimepkg.ErrNotFound) {
			return store.RuntimeInstance{}, err
		}
	}
	return s.Create(ctx, projectID, issueID, runtimeID)
}
