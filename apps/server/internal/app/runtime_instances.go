package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const orphanRuntimeCleanupTimeout = 30 * time.Second

type RuntimeInstanceStore interface {
	GetRuntime(context.Context, *string, string) (store.Runtime, error)
	CreateRuntimeInstance(context.Context, store.RuntimeInstance) (store.RuntimeInstance, error)
	GetRuntimeInstance(context.Context, string, string) (store.RuntimeInstance, error)
	UpdateRuntimeInstanceState(context.Context, string, string, string, *string, string, json.RawMessage) (store.RuntimeInstance, error)
}

type IssueWorkspaceEnsurer interface {
	EnsureIssueWorkspace(context.Context, string, string) (store.Workspace, error)
}

type RuntimeInstanceService struct {
	store           RuntimeInstanceStore
	workspaces      IssueWorkspaceEnsurer
	implementations map[string]runtimepkg.Implementation
}

func NewRuntimeInstanceService(runtimeStore RuntimeInstanceStore, workspaces IssueWorkspaceEnsurer, implementations map[string]runtimepkg.Implementation) (*RuntimeInstanceService, error) {
	if runtimeStore == nil || workspaces == nil {
		return nil, fmt.Errorf("runtime instance service dependencies are required")
	}
	if len(implementations) == 0 {
		return nil, fmt.Errorf("at least one Runtime implementation is required")
	}
	copyImplementations := make(map[string]runtimepkg.Implementation, len(implementations))
	for kind, implementation := range implementations {
		kind = strings.TrimSpace(kind)
		if kind == "" || implementation == nil {
			return nil, fmt.Errorf("runtime implementation kind and value are required")
		}
		copyImplementations[kind] = implementation
	}
	return &RuntimeInstanceService{store: runtimeStore, workspaces: workspaces, implementations: copyImplementations}, nil
}

func (s *RuntimeInstanceService) Create(ctx context.Context, projectID, issueID, runtimeID string) (store.RuntimeInstance, error) {
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
	runtimeConfig, implementation, err := s.resolveImplementation(ctx, projectID, runtimeID)
	if err != nil {
		return store.RuntimeInstance{}, err
	}

	instance, err := s.store.CreateRuntimeInstance(ctx, store.RuntimeInstance{
		ProjectID:    projectID,
		WorkspaceID:  workspace.ID,
		RuntimeID:    runtimeConfig.ID,
		Status:       string(runtimepkg.StateProvisioning),
		RunnerStatus: "CONNECTING",
	})
	if err != nil {
		return store.RuntimeInstance{}, translateStoreError(err, "runtime_instance")
	}
	spec := runtimeSpec(instance, workspace, issueID, runtimeConfig)
	handle, err := implementation.Create(ctx, spec)
	if err != nil {
		return s.failInstance(ctx, instance, err)
	}
	externalID := handle.ExternalID
	instance, err = s.updateState(ctx, instance, runtimepkg.StateProvisioning, &externalID, "CONNECTING", handle.Metadata)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), orphanRuntimeCleanupTimeout)
		defer cancel()
		if destroyErr := implementation.Destroy(cleanupCtx, handle); destroyErr != nil {
			return store.RuntimeInstance{}, errors.Join(err, fmt.Errorf("destroy orphaned Runtime instance: %w", destroyErr))
		}
		return store.RuntimeInstance{}, err
	}
	return instance, nil
}

func (s *RuntimeInstanceService) Start(ctx context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	instance, implementation, handle, err := s.loadInstance(ctx, projectID, instanceID)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	if instance.Status == string(runtimepkg.StateRunning) {
		return instance, nil
	}
	if instance.Status == string(runtimepkg.StateDestroyed) {
		return store.RuntimeInstance{}, NewError("runtime_instance_destroyed", "Runtime Instance is already destroyed", runtimepkg.ErrInvalidTransition)
	}
	instance, err = s.updateState(ctx, instance, runtimepkg.StateStarting, instance.ExternalID, "CONNECTING", instance.SafeHandleMetadata)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	if err := implementation.Start(ctx, handle); err != nil {
		return s.failInstance(ctx, instance, err)
	}
	inspection, err := implementation.Inspect(ctx, handle)
	if err != nil {
		return s.failInstance(ctx, instance, err)
	}
	if inspection.State != runtimepkg.StateRunning {
		return s.failInstance(ctx, instance, fmt.Errorf("runtime instance did not reach RUNNING state: %s", inspection.State))
	}
	return s.updateState(ctx, instance, runtimepkg.StateRunning, instance.ExternalID, "CONNECTING", instance.SafeHandleMetadata)
}

func (s *RuntimeInstanceService) Inspect(ctx context.Context, projectID, instanceID string) (runtimepkg.Inspection, error) {
	_, implementation, handle, err := s.loadInstance(ctx, projectID, instanceID)
	if err != nil {
		return runtimepkg.Inspection{}, err
	}
	return implementation.Inspect(ctx, handle)
}

func (s *RuntimeInstanceService) Stop(ctx context.Context, projectID, instanceID string, reason runtimepkg.StopReason) (store.RuntimeInstance, error) {
	instance, implementation, handle, err := s.loadInstance(ctx, projectID, instanceID)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	switch runtimepkg.State(instance.Status) {
	case runtimepkg.StateStopped, runtimepkg.StateDestroyed:
		return instance, nil
	case runtimepkg.StateProvisioning:
		return store.RuntimeInstance{}, NewError("runtime_instance_not_started", "Runtime Instance has not been started", runtimepkg.ErrInvalidTransition)
	}
	instance, err = s.updateState(ctx, instance, runtimepkg.StateStopping, instance.ExternalID, "DRAINING", instance.SafeHandleMetadata)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	if err := implementation.Stop(ctx, handle, reason); err != nil {
		return s.failInstance(ctx, instance, err)
	}
	return s.updateState(ctx, instance, runtimepkg.StateStopped, instance.ExternalID, "UNAVAILABLE", instance.SafeHandleMetadata)
}

func (s *RuntimeInstanceService) Destroy(ctx context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	instance, err := s.getInstance(ctx, projectID, instanceID)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	if instance.Status == string(runtimepkg.StateDestroyed) {
		return instance, nil
	}
	if instance.Status == string(runtimepkg.StateStarting) || instance.Status == string(runtimepkg.StateRunning) || instance.Status == string(runtimepkg.StateStopping) {
		instance, err = s.Stop(ctx, projectID, instanceID, runtimepkg.StopReasonRequested)
		if err != nil {
			return store.RuntimeInstance{}, err
		}
	}
	implementation, handle, err := s.implementationAndHandle(ctx, instance)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	if err := implementation.Destroy(ctx, handle); err != nil {
		return s.failInstance(ctx, instance, err)
	}
	return s.updateState(ctx, instance, runtimepkg.StateDestroyed, instance.ExternalID, "UNAVAILABLE", instance.SafeHandleMetadata)
}

func (s *RuntimeInstanceService) getInstance(ctx context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(instanceID) == "" {
		return store.RuntimeInstance{}, NewError("invalid_argument", "projectId and runtimeInstanceId are required", store.ErrInvalidArgument)
	}
	instance, err := s.store.GetRuntimeInstance(ctx, projectID, instanceID)
	return instance, translateStoreError(err, "runtime_instance")
}

func (s *RuntimeInstanceService) loadInstance(ctx context.Context, projectID, instanceID string) (store.RuntimeInstance, runtimepkg.Implementation, runtimepkg.Handle, error) {
	instance, err := s.getInstance(ctx, projectID, instanceID)
	if err != nil {
		return store.RuntimeInstance{}, nil, runtimepkg.Handle{}, err
	}
	implementation, handle, err := s.implementationAndHandle(ctx, instance)
	return instance, implementation, handle, err
}

func (s *RuntimeInstanceService) implementationAndHandle(ctx context.Context, instance store.RuntimeInstance) (runtimepkg.Implementation, runtimepkg.Handle, error) {
	_, implementation, err := s.resolveImplementation(ctx, instance.ProjectID, instance.RuntimeID)
	if err != nil {
		return nil, runtimepkg.Handle{}, err
	}
	if instance.ExternalID == nil || strings.TrimSpace(*instance.ExternalID) == "" || len(instance.SafeHandleMetadata) == 0 {
		return nil, runtimepkg.Handle{}, NewError("runtime_instance_unreconciled", "Runtime Instance external identity is unavailable; reconcile it before lifecycle operations", runtimepkg.ErrNotFound)
	}
	return implementation, runtimepkg.Handle{ExternalID: *instance.ExternalID, Metadata: instance.SafeHandleMetadata}, nil
}

func (s *RuntimeInstanceService) resolveImplementation(ctx context.Context, projectID, runtimeID string) (store.Runtime, runtimepkg.Implementation, error) {
	runtimeConfig, err := s.store.GetRuntime(ctx, &projectID, runtimeID)
	if err != nil {
		return store.Runtime{}, nil, translateStoreError(err, "runtime")
	}
	if !runtimeConfig.Enabled {
		return store.Runtime{}, nil, NewError("runtime_disabled", "Runtime is disabled", store.ErrInvalidArgument)
	}
	implementation := s.implementations[runtimeConfig.Kind]
	if implementation == nil {
		return store.Runtime{}, nil, NewError("runtime_unsupported", "Runtime implementation is not configured", runtimepkg.ErrUnsupportedPolicy)
	}
	return runtimeConfig, implementation, nil
}

func (s *RuntimeInstanceService) updateState(ctx context.Context, instance store.RuntimeInstance, next runtimepkg.State, externalID *string, runnerStatus string, metadata json.RawMessage) (store.RuntimeInstance, error) {
	if err := runtimepkg.ValidateTransition(runtimepkg.State(instance.Status), next); err != nil {
		return store.RuntimeInstance{}, err
	}
	updated, err := s.store.UpdateRuntimeInstanceState(ctx, instance.ProjectID, instance.ID, string(next), externalID, runnerStatus, metadata)
	if err != nil {
		return store.RuntimeInstance{}, translateStoreError(err, "runtime_instance")
	}
	if updated.WorkspaceID != instance.WorkspaceID || updated.RuntimeID != instance.RuntimeID {
		return store.RuntimeInstance{}, fmt.Errorf("runtime instance immutable binding changed during state update")
	}
	return updated, nil
}

func (s *RuntimeInstanceService) failInstance(ctx context.Context, instance store.RuntimeInstance, cause error) (store.RuntimeInstance, error) {
	if instance.Status != string(runtimepkg.StateFailed) {
		updated, updateErr := s.updateState(ctx, instance, runtimepkg.StateFailed, instance.ExternalID, "UNAVAILABLE", instance.SafeHandleMetadata)
		if updateErr != nil {
			return store.RuntimeInstance{}, errors.Join(cause, fmt.Errorf("persist Runtime Instance failure: %w", updateErr))
		}
		instance = updated
	}
	return instance, cause
}

func runtimeSpec(instance store.RuntimeInstance, workspace store.Workspace, issueID string, runtimeConfig store.Runtime) runtimepkg.RuntimeSpec {
	return runtimepkg.RuntimeSpec{
		RuntimeInstanceID: instance.ID,
		ProjectID:         instance.ProjectID,
		IssueID:           issueID,
		WorkspaceID:       workspace.ID,
		RuntimeID:         runtimeConfig.ID,
		Image:             runtimeConfig.Image,
		WorkingDirectory:  runtimepkg.WorkspaceTarget,
		Resources: runtimepkg.ResourcePolicy{
			CPULimitMillis:   runtimeConfig.CPULimitMillis,
			MemoryLimitBytes: runtimeConfig.MemoryLimitBytes,
			PIDLimit:         runtimeConfig.PIDLimit,
			TimeoutSeconds:   runtimeConfig.TimeoutSeconds,
		},
		Workspace: runtimepkg.WorkspaceMount{
			WorkspaceID: workspace.ID,
			Source:      workspace.Path,
			Target:      runtimepkg.WorkspaceTarget,
		},
		Network:           runtimepkg.NetworkPolicy(runtimeConfig.NetworkPolicy),
		AllowedSecretRefs: append([]string(nil), runtimeConfig.AllowedSecretRefs...),
	}
}
