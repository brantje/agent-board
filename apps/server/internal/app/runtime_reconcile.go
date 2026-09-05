package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type RuntimeReconcileStore interface {
	RuntimeInstanceStore
	ListProjects(context.Context) ([]store.Project, error)
	ListRuntimeInstances(context.Context, string, []string) ([]store.RuntimeInstance, error)
	GetWorkspace(context.Context, string, string) (store.Workspace, error)
}

// ReconcileAll reconciles non-terminal Runtime Instances after server restart.
// It uses durable external identity when available and deterministic recovery
// when a crash happened before the external ID was persisted.
func (s *RuntimeInstanceService) ReconcileAll(ctx context.Context) error {
	reconcileStore, ok := s.store.(RuntimeReconcileStore)
	if !ok {
		return fmt.Errorf("runtime instance store does not support reconciliation")
	}
	projects, err := reconcileStore.ListProjects(ctx)
	if err != nil {
		return translateStoreError(err, "project")
	}
	var reconcileErrors []error
	statuses := []string{
		string(runtimepkg.StateProvisioning),
		string(runtimepkg.StateStarting),
		string(runtimepkg.StateRunning),
		string(runtimepkg.StateStopping),
	}
	for _, project := range projects {
		instances, err := reconcileStore.ListRuntimeInstances(ctx, project.ID, statuses)
		if err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("list Runtime Instances for project %s: %w", project.ID, err))
			continue
		}
		for _, instance := range instances {
			if _, err := s.reconcile(ctx, reconcileStore, instance); err != nil {
				reconcileErrors = append(reconcileErrors, fmt.Errorf("reconcile Runtime Instance %s: %w", instance.ID, err))
			}
		}
	}
	return errors.Join(reconcileErrors...)
}

func (s *RuntimeInstanceService) Reconcile(ctx context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	reconcileStore, ok := s.store.(RuntimeReconcileStore)
	if !ok {
		return store.RuntimeInstance{}, fmt.Errorf("runtime instance store does not support reconciliation")
	}
	instance, err := s.getInstance(ctx, projectID, instanceID)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	return s.reconcile(ctx, reconcileStore, instance)
}

func (s *RuntimeInstanceService) reconcile(ctx context.Context, reconcileStore RuntimeReconcileStore, instance store.RuntimeInstance) (store.RuntimeInstance, error) {
	if instance.Status == string(runtimepkg.StateDestroyed) {
		return instance, nil
	}
	runtimeConfig, implementation, err := s.resolveForReconciliation(ctx, instance.ProjectID, instance.RuntimeID)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	workspace, err := reconcileStore.GetWorkspace(ctx, instance.ProjectID, instance.WorkspaceID)
	if err != nil {
		return store.RuntimeInstance{}, translateStoreError(err, "workspace")
	}
	if workspace.ProjectID != instance.ProjectID || workspace.ID != instance.WorkspaceID {
		return store.RuntimeInstance{}, fmt.Errorf("runtime instance workspace binding does not match persisted Workspace")
	}
	spec := runtimeSpec(instance, workspace, workspace.IssueID, runtimeConfig)

	var handle runtimepkg.Handle
	var inspection runtimepkg.Inspection
	if instance.ExternalID != nil && strings.TrimSpace(*instance.ExternalID) != "" && len(instance.SafeHandleMetadata) != 0 {
		handle = runtimepkg.Handle{ExternalID: *instance.ExternalID, Metadata: instance.SafeHandleMetadata}
		inspection, err = implementation.Inspect(ctx, handle)
	} else {
		recovering, ok := implementation.(runtimepkg.RecoveringImplementation)
		if !ok {
			return store.RuntimeInstance{}, NewError("runtime_recovery_unsupported", "Runtime implementation cannot recover missing external identity", runtimepkg.ErrUnsupportedPolicy)
		}
		handle, inspection, err = recovering.Recover(ctx, spec)
	}
	if errors.Is(err, runtimepkg.ErrNotFound) {
		return s.reconcileState(ctx, instance, runtimepkg.StateFailed, instance.ExternalID, instance.SafeHandleMetadata)
	}
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	externalID := handle.ExternalID
	return s.reconcileState(ctx, instance, inspection.State, &externalID, handle.Metadata)
}

func (s *RuntimeInstanceService) resolveForReconciliation(ctx context.Context, projectID, runtimeID string) (store.Runtime, runtimepkg.Implementation, error) {
	runtimeConfig, err := s.store.GetRuntime(ctx, &projectID, runtimeID)
	if err != nil {
		return store.Runtime{}, nil, translateStoreError(err, "runtime")
	}
	implementation := s.implementations[runtimeConfig.Kind]
	if implementation == nil {
		return store.Runtime{}, nil, NewError("runtime_unsupported", "Runtime implementation is not configured", runtimepkg.ErrUnsupportedPolicy)
	}
	return runtimeConfig, implementation, nil
}

func (s *RuntimeInstanceService) reconcileState(ctx context.Context, instance store.RuntimeInstance, observed runtimepkg.State, externalID *string, metadata json.RawMessage) (store.RuntimeInstance, error) {
	switch observed {
	case runtimepkg.StateProvisioning, runtimepkg.StateStarting, runtimepkg.StateRunning, runtimepkg.StateStopping, runtimepkg.StateFailed, runtimepkg.StateStopped:
	default:
		return store.RuntimeInstance{}, fmt.Errorf("cannot reconcile unsupported observed Runtime state %q", observed)
	}
	runnerStatus := "CONNECTING"
	if observed == runtimepkg.StateStopping {
		runnerStatus = "DRAINING"
	}
	if observed == runtimepkg.StateFailed || observed == runtimepkg.StateStopped {
		runnerStatus = "UNAVAILABLE"
	}
	updated, err := s.store.UpdateRuntimeInstanceState(ctx, instance.ProjectID, instance.ID, string(observed), externalID, runnerStatus, metadata)
	if err != nil {
		return store.RuntimeInstance{}, translateStoreError(err, "runtime_instance")
	}
	if updated.WorkspaceID != instance.WorkspaceID || updated.RuntimeID != instance.RuntimeID {
		return store.RuntimeInstance{}, fmt.Errorf("runtime instance immutable binding changed during reconciliation")
	}
	return updated, nil
}

func (s *RuntimeInstanceService) Close() error {
	var closeErrors []error
	for _, implementation := range s.implementations {
		if closer, ok := implementation.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}
	}
	return errors.Join(closeErrors...)
}
