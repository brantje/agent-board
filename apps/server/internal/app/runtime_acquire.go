package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runtimeInstanceLister interface {
	ListRuntimeInstances(context.Context, string, []string) ([]store.RuntimeInstance, error)
}

var runtimeAcquisitionStatuses = []string{
	string(runtimepkg.StateProvisioning),
	string(runtimepkg.StateStarting),
	string(runtimepkg.StateRunning),
	string(runtimepkg.StateStopping),
	string(runtimepkg.StateStopped),
}

// Acquire returns reusable compute for the Issue Workspace when possible. The
// distributed acquisition lock covers reuse, crash reconciliation, provisioning
// and initial start so concurrent resumptions cannot provision duplicate compute
// for the same Workspace/Runtime pair.
func (s *RuntimeInstanceService) Acquire(ctx context.Context, projectID, issueID, runtimeID string) (result store.RuntimeInstance, err error) {
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
	runtimeConfig, _, err := s.resolveImplementation(ctx, projectID, runtimeID)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	locker, ok := s.store.(store.RuntimeAcquisitionStore)
	if !ok {
		return store.RuntimeInstance{}, NewError("runtime_acquisition_unsupported", "Runtime Instance store does not support atomic acquisition", runtimepkg.ErrUnsupportedPolicy)
	}
	lock, err := locker.AcquireRuntimeAcquisitionLock(ctx, workspace.ID, runtimeConfig.ID)
	if err != nil {
		return store.RuntimeInstance{}, translateStoreError(err, "runtime_instance")
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			result = store.RuntimeInstance{}
			err = errors.Join(err, fmt.Errorf("release Runtime acquisition lock: %w", releaseErr))
		}
	}()

	return s.acquireRuntimeLocked(ctx, projectID, issueID, workspace.ID, runtimeConfig.ID)
}

func (s *RuntimeInstanceService) acquireRuntimeLocked(ctx context.Context, projectID, issueID, workspaceID, runtimeID string) (store.RuntimeInstance, error) {
	lister, ok := s.store.(runtimeInstanceLister)
	if !ok {
		return s.createAndStartRuntime(ctx, projectID, issueID, runtimeID)
	}
	instances, err := lister.ListRuntimeInstances(ctx, projectID, runtimeAcquisitionStatuses)
	if err != nil {
		return store.RuntimeInstance{}, translateStoreError(err, "runtime_instance")
	}

	for i := len(instances) - 1; i >= 0; i-- {
		instance := instances[i]
		if instance.WorkspaceID != workspaceID || instance.RuntimeID != runtimeID {
			continue
		}
		reused, reusable, err := s.reuseRuntimeInstanceLocked(ctx, projectID, instance)
		if err != nil {
			return store.RuntimeInstance{}, err
		}
		if reusable {
			return reused, nil
		}
	}
	return s.createAndStartRuntime(ctx, projectID, issueID, runtimeID)
}

func (s *RuntimeInstanceService) reuseRuntimeInstanceLocked(ctx context.Context, projectID string, instance store.RuntimeInstance) (store.RuntimeInstance, bool, error) {
	switch runtimepkg.State(instance.Status) {
	case runtimepkg.StateRunning:
		inspection, err := s.Inspect(ctx, projectID, instance.ID)
		if err == nil && inspection.State == runtimepkg.StateRunning {
			return instance, true, nil
		}
		if err != nil && !errors.Is(err, runtimepkg.ErrNotFound) {
			return store.RuntimeInstance{}, false, err
		}
		return s.reconcileRuntimeForAcquisition(ctx, projectID, instance)
	case runtimepkg.StateStopped:
		started, err := s.Start(ctx, projectID, instance.ID)
		if err == nil {
			return started, true, nil
		}
		if !errors.Is(err, runtimepkg.ErrNotFound) {
			return store.RuntimeInstance{}, false, err
		}
		return s.reconcileRuntimeForAcquisition(ctx, projectID, instance)
	case runtimepkg.StateProvisioning, runtimepkg.StateStarting, runtimepkg.StateStopping:
		return s.reconcileRuntimeForAcquisition(ctx, projectID, instance)
	default:
		return store.RuntimeInstance{}, false, nil
	}
}

func (s *RuntimeInstanceService) reconcileRuntimeForAcquisition(ctx context.Context, projectID string, instance store.RuntimeInstance) (store.RuntimeInstance, bool, error) {
	reconciled, err := s.Reconcile(ctx, projectID, instance.ID)
	if err != nil {
		return store.RuntimeInstance{}, false, err
	}
	switch runtimepkg.State(reconciled.Status) {
	case runtimepkg.StateRunning:
		return reconciled, true, nil
	case runtimepkg.StateStopped:
		started, err := s.Start(ctx, projectID, reconciled.ID)
		if err != nil {
			return store.RuntimeInstance{}, false, err
		}
		return started, true, nil
	case runtimepkg.StateFailed, runtimepkg.StateDestroyed:
		return store.RuntimeInstance{}, false, nil
	case runtimepkg.StateProvisioning, runtimepkg.StateStarting, runtimepkg.StateStopping:
		return store.RuntimeInstance{}, false, NewError("runtime_acquisition_unsettled", "Runtime Instance reconciliation left acquisition in a transitional state", store.ErrConflict)
	default:
		return store.RuntimeInstance{}, false, NewError("runtime_acquisition_invalid_state", "Runtime Instance has an unsupported acquisition state", store.ErrConflict)
	}
}

func (s *RuntimeInstanceService) createAndStartRuntime(ctx context.Context, projectID, issueID, runtimeID string) (store.RuntimeInstance, error) {
	created, err := s.Create(ctx, projectID, issueID, runtimeID)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	return s.Start(ctx, projectID, created.ID)
}
