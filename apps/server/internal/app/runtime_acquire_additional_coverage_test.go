package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type failingReleaseRuntimeLock struct{ err error }

func (l failingReleaseRuntimeLock) Release() error { return l.err }

type releaseFailRuntimeStore struct {
	*listingRuntimeStore
	err error
}

func (s *releaseFailRuntimeStore) AcquireRuntimeAcquisitionLock(context.Context, string, string) (store.RuntimeAcquisitionLock, error) {
	return failingReleaseRuntimeLock{err: s.err}, nil
}

type failingListAndReleaseRuntimeStore struct {
	*runtimeServiceStore
	listErr    error
	releaseErr error
}

func (s *failingListAndReleaseRuntimeStore) ListRuntimeInstances(context.Context, string, []string) ([]store.RuntimeInstance, error) {
	return nil, s.listErr
}

func (s *failingListAndReleaseRuntimeStore) AcquireRuntimeAcquisitionLock(context.Context, string, string) (store.RuntimeAcquisitionLock, error) {
	return failingReleaseRuntimeLock{err: s.releaseErr}, nil
}

type lockErrorRuntimeStore struct {
	*listingRuntimeStore
	err error
}

func (s *lockErrorRuntimeStore) AcquireRuntimeAcquisitionLock(context.Context, string, string) (store.RuntimeAcquisitionLock, error) {
	return nil, s.err
}

type stoppedRecoveringRuntime struct {
	fakeRuntimeImplementation
	recoverCalls int
}

func (r *stoppedRecoveringRuntime) Recover(context.Context, runtimepkg.RuntimeSpec) (runtimepkg.Handle, runtimepkg.Inspection, error) {
	r.recoverCalls++
	handle := runtimepkg.Handle{ExternalID: "recovered-stopped", Metadata: json.RawMessage(`{"safe":true}`)}
	return handle, runtimepkg.Inspection{ExternalID: handle.ExternalID, State: runtimepkg.StateStopped}, nil
}

type missingRecoveringRuntime struct {
	fakeRuntimeImplementation
	recoverCalls int
}

func (r *missingRecoveringRuntime) Recover(context.Context, runtimepkg.RuntimeSpec) (runtimepkg.Handle, runtimepkg.Inspection, error) {
	r.recoverCalls++
	return runtimepkg.Handle{}, runtimepkg.Inspection{}, runtimepkg.ErrNotFound
}

type provisioningRecoveringRuntime struct {
	fakeRuntimeImplementation
	recoverCalls int
}

func (r *provisioningRecoveringRuntime) Recover(context.Context, runtimepkg.RuntimeSpec) (runtimepkg.Handle, runtimepkg.Inspection, error) {
	r.recoverCalls++
	handle := runtimepkg.Handle{ExternalID: "recovered-provisioning", Metadata: json.RawMessage(`{"safe":true}`)}
	return handle, runtimepkg.Inspection{ExternalID: handle.ExternalID, State: runtimepkg.StateProvisioning}, nil
}

type disappearingRunningRuntime struct {
	fakeRuntimeImplementation
	inspectCalls int
}

func (r *disappearingRunningRuntime) Inspect(ctx context.Context, handle runtimepkg.Handle) (runtimepkg.Inspection, error) {
	r.inspectCalls++
	if r.inspectCalls <= 2 {
		return runtimepkg.Inspection{}, runtimepkg.ErrNotFound
	}
	return r.fakeRuntimeImplementation.Inspect(ctx, handle)
}

type acquireInspectErrorRuntime struct {
	fakeRuntimeImplementation
	err error
}

func (r *acquireInspectErrorRuntime) Inspect(context.Context, runtimepkg.Handle) (runtimepkg.Inspection, error) {
	return runtimepkg.Inspection{}, r.err
}

func TestRuntimeInstanceServiceAcquireWithoutListerCreatesAndStartsRuntime(t *testing.T) {
	service, baseStore, implementation, workspace := runtimeServiceFixture(t)

	instance, err := service.Acquire(t.Context(), workspace.ProjectID, workspace.IssueID, baseStore.runtime.ID)
	if err != nil {
		t.Fatalf("Acquire() error=%v", err)
	}
	if instance.Status != string(runtimepkg.StateRunning) || instance.WorkspaceID != workspace.ID {
		t.Fatalf("Acquire()=%+v", instance)
	}
	if implementation.startCalls != 1 || implementation.createdSpec.RuntimeInstanceID != instance.ID {
		t.Fatalf("startCalls=%d created=%+v", implementation.startCalls, implementation.createdSpec)
	}
}

func TestRuntimeInstanceServiceAcquireReportsLockReleaseFailure(t *testing.T) {
	_, baseStore, implementation, workspace := runtimeServiceFixture(t)
	externalID := "container-existing"
	baseStore.instance = store.RuntimeInstance{
		ID:                 "instance-running",
		ProjectID:          workspace.ProjectID,
		WorkspaceID:        workspace.ID,
				Status:             string(runtimepkg.StateRunning),
		ExternalID:         &externalID,
		RunnerStatus:       "READY",
		SafeHandleMetadata: json.RawMessage(`{"safe":true}`),
	}
	releaseErr := errors.New("unlock failed")
	service, err := NewRuntimeInstanceService(
		&releaseFailRuntimeStore{listingRuntimeStore: &listingRuntimeStore{runtimeServiceStore: baseStore}, err: releaseErr},
		&runtimeWorkspaceEnsurer{workspace: workspace},
		map[string]runtimepkg.Implementation{"docker": implementation},
	)
	if err != nil {
		t.Fatal(err)
	}

	instance, err := service.Acquire(t.Context(), workspace.ProjectID, workspace.IssueID, baseStore.runtime.ID)
	if !errors.Is(err, releaseErr) || !strings.Contains(err.Error(), "release Runtime acquisition lock") {
		t.Fatalf("Acquire() error=%v", err)
	}
	if instance.ID != "" {
		t.Fatalf("Acquire() must discard result when lock release fails: %+v", instance)
	}
}

func TestRuntimeInstanceServiceAcquireJoinsOperationAndLockReleaseFailures(t *testing.T) {
	_, baseStore, implementation, workspace := runtimeServiceFixture(t)
	releaseErr := errors.New("unlock failed")
	service, err := NewRuntimeInstanceService(
		&failingListAndReleaseRuntimeStore{runtimeServiceStore: baseStore, listErr: store.ErrNotFound, releaseErr: releaseErr},
		&runtimeWorkspaceEnsurer{workspace: workspace},
		map[string]runtimepkg.Implementation{"docker": implementation},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Acquire(t.Context(), workspace.ProjectID, workspace.IssueID, baseStore.runtime.ID)
	if !errors.Is(err, store.ErrNotFound) || !errors.Is(err, releaseErr) {
		t.Fatalf("Acquire() joined error=%v", err)
	}
	if !strings.Contains(err.Error(), "release Runtime acquisition lock") {
		t.Fatalf("Acquire() missing release context: %v", err)
	}
}

func TestRuntimeInstanceServiceAcquireReportsLockAcquisitionFailure(t *testing.T) {
	_, baseStore, implementation, workspace := runtimeServiceFixture(t)
	service, err := NewRuntimeInstanceService(
		&lockErrorRuntimeStore{listingRuntimeStore: &listingRuntimeStore{runtimeServiceStore: baseStore}, err: store.ErrConflict},
		&runtimeWorkspaceEnsurer{workspace: workspace},
		map[string]runtimepkg.Implementation{"docker": implementation},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Acquire(t.Context(), workspace.ProjectID, workspace.IssueID, baseStore.runtime.ID)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Acquire() error=%v", err)
	}
	if implementation.createdSpec.RuntimeInstanceID != "" || implementation.startCalls != 0 {
		t.Fatalf("lock failure must not provision compute: startCalls=%d created=%+v", implementation.startCalls, implementation.createdSpec)
	}
}

func TestRuntimeInstanceServiceAcquireRestartsRecoveredStoppedRuntime(t *testing.T) {
	projectID := "project-1"
	workspace := store.Workspace{ID: "workspace-1", ProjectID: projectID, IssueID: "issue-1", Path: "/workspaces/one", BootstrapStatus: "READY"}
	runtimeConfig := store.Runtime{ID: "runtime-1", ProjectID: &projectID, Kind: "docker", Image: "runtime:test", NetworkPolicy: "none", WorkspacePolicy: "issue", Enabled: true}
	base := &runtimeServiceStore{runtime: runtimeConfig, instance: store.RuntimeInstance{
		ID: "instance-interrupted", ProjectID: projectID, WorkspaceID: workspace.ID,
		Status: string(runtimepkg.StateProvisioning), RunnerStatus: "CONNECTING",
	}}
	rs := &reconcileStore{runtimeServiceStore: base, workspace: workspace}
	implementation := &stoppedRecoveringRuntime{}
	service, err := NewRuntimeInstanceService(rs, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}

	instance, err := service.Acquire(t.Context(), projectID, workspace.IssueID, runtimeConfig.ID)
	if err != nil {
		t.Fatalf("Acquire() error=%v", err)
	}
	if instance.ID != "instance-interrupted" || instance.Status != string(runtimepkg.StateRunning) {
		t.Fatalf("Acquire()=%+v", instance)
	}
	if implementation.recoverCalls != 1 || implementation.startCalls != 1 || implementation.createdSpec.RuntimeInstanceID != "" {
		t.Fatalf("recoverCalls=%d startCalls=%d created=%+v", implementation.recoverCalls, implementation.startCalls, implementation.createdSpec)
	}
}

func TestRuntimeInstanceServiceAcquireReplacesRecoveredMissingRuntime(t *testing.T) {
	projectID := "project-1"
	workspace := store.Workspace{ID: "workspace-1", ProjectID: projectID, IssueID: "issue-1", Path: "/workspaces/one", BootstrapStatus: "READY"}
	runtimeConfig := store.Runtime{ID: "runtime-1", ProjectID: &projectID, Kind: "docker", Image: "runtime:test", NetworkPolicy: "none", WorkspacePolicy: "issue", Enabled: true}
	base := &runtimeServiceStore{runtime: runtimeConfig, instance: store.RuntimeInstance{
		ID: "instance-missing", ProjectID: projectID, WorkspaceID: workspace.ID,
		Status: string(runtimepkg.StateProvisioning), RunnerStatus: "CONNECTING",
	}}
	rs := &reconcileStore{runtimeServiceStore: base, workspace: workspace}
	implementation := &missingRecoveringRuntime{}
	service, err := NewRuntimeInstanceService(rs, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}

	instance, err := service.Acquire(t.Context(), projectID, workspace.IssueID, runtimeConfig.ID)
	if err != nil {
		t.Fatalf("Acquire() error=%v", err)
	}
	if instance.Status != string(runtimepkg.StateRunning) || instance.WorkspaceID != workspace.ID {
		t.Fatalf("Acquire() replacement=%+v", instance)
	}
	if implementation.recoverCalls != 1 || implementation.startCalls != 1 || implementation.createdSpec.RuntimeInstanceID != instance.ID {
		t.Fatalf("recoverCalls=%d startCalls=%d created=%+v", implementation.recoverCalls, implementation.startCalls, implementation.createdSpec)
	}
}

func TestRuntimeInstanceServiceAcquireRejectsUnsettledRecoveredRuntime(t *testing.T) {
	projectID := "project-1"
	workspace := store.Workspace{ID: "workspace-1", ProjectID: projectID, IssueID: "issue-1", Path: "/workspaces/one", BootstrapStatus: "READY"}
	runtimeConfig := store.Runtime{ID: "runtime-1", ProjectID: &projectID, Kind: "docker", Image: "runtime:test", NetworkPolicy: "none", WorkspacePolicy: "issue", Enabled: true}
	base := &runtimeServiceStore{runtime: runtimeConfig, instance: store.RuntimeInstance{
		ID: "instance-unsettled", ProjectID: projectID, WorkspaceID: workspace.ID,
		Status: string(runtimepkg.StateProvisioning), RunnerStatus: "CONNECTING",
	}}
	rs := &reconcileStore{runtimeServiceStore: base, workspace: workspace}
	implementation := &provisioningRecoveringRuntime{}
	service, err := NewRuntimeInstanceService(rs, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Acquire(t.Context(), projectID, workspace.IssueID, runtimeConfig.ID)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Acquire() error=%v", err)
	}
	if implementation.recoverCalls != 1 || implementation.createdSpec.RuntimeInstanceID != "" || implementation.startCalls != 0 {
		t.Fatalf("unsettled recovery must not provision: recoverCalls=%d startCalls=%d created=%+v", implementation.recoverCalls, implementation.startCalls, implementation.createdSpec)
	}
}

func TestRuntimeInstanceServiceAcquireReplacesDisappearedRunningRuntime(t *testing.T) {
	projectID := "project-1"
	workspace := store.Workspace{ID: "workspace-1", ProjectID: projectID, IssueID: "issue-1", Path: "/workspaces/one", BootstrapStatus: "READY"}
	runtimeConfig := store.Runtime{ID: "runtime-1", ProjectID: &projectID, Kind: "docker", Image: "runtime:test", NetworkPolicy: "none", WorkspacePolicy: "issue", Enabled: true}
	externalID := "container-disappeared"
	base := &runtimeServiceStore{runtime: runtimeConfig, instance: store.RuntimeInstance{
		ID: "instance-running", ProjectID: projectID, WorkspaceID: workspace.ID,
		Status: string(runtimepkg.StateRunning), ExternalID: &externalID, RunnerStatus: "READY", SafeHandleMetadata: json.RawMessage(`{"safe":true}`),
	}}
	rs := &reconcileStore{runtimeServiceStore: base, workspace: workspace}
	implementation := &disappearingRunningRuntime{}
	service, err := NewRuntimeInstanceService(rs, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}

	instance, err := service.Acquire(t.Context(), projectID, workspace.IssueID, runtimeConfig.ID)
	if err != nil {
		t.Fatalf("Acquire() error=%v", err)
	}
	if instance.Status != string(runtimepkg.StateRunning) || instance.WorkspaceID != workspace.ID {
		t.Fatalf("Acquire() replacement=%+v", instance)
	}
	if implementation.inspectCalls != 3 || implementation.startCalls != 1 || implementation.createdSpec.RuntimeInstanceID != instance.ID {
		t.Fatalf("inspectCalls=%d startCalls=%d created=%+v", implementation.inspectCalls, implementation.startCalls, implementation.createdSpec)
	}
}

func TestRuntimeInstanceServiceAcquireReturnsRunningInspectionFailure(t *testing.T) {
	projectID := "project-1"
	workspace := store.Workspace{ID: "workspace-1", ProjectID: projectID, IssueID: "issue-1", Path: "/workspaces/one", BootstrapStatus: "READY"}
	runtimeConfig := store.Runtime{ID: "runtime-1", ProjectID: &projectID, Kind: "docker", Image: "runtime:test", NetworkPolicy: "none", WorkspacePolicy: "issue", Enabled: true}
	externalID := "container-running"
	base := &runtimeServiceStore{runtime: runtimeConfig, instance: store.RuntimeInstance{
		ID: "instance-running", ProjectID: projectID, WorkspaceID: workspace.ID,
		Status: string(runtimepkg.StateRunning), ExternalID: &externalID, RunnerStatus: "READY", SafeHandleMetadata: json.RawMessage(`{"safe":true}`),
	}}
	implementationErr := errors.New("inspect transport unavailable")
	implementation := &acquireInspectErrorRuntime{err: implementationErr}
	service, err := NewRuntimeInstanceService(&listingRuntimeStore{runtimeServiceStore: base}, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Acquire(t.Context(), projectID, workspace.IssueID, runtimeConfig.ID)
	if !errors.Is(err, implementationErr) {
		t.Fatalf("Acquire() error=%v", err)
	}
	if implementation.createdSpec.RuntimeInstanceID != "" || implementation.startCalls != 0 {
		t.Fatalf("inspection failure must not provision replacement: startCalls=%d created=%+v", implementation.startCalls, implementation.createdSpec)
	}
}

func TestRuntimeInstanceServiceAcquireReportsMissingRuntimeConfiguration(t *testing.T) {
	service, _, implementation, workspace := runtimeServiceFixture(t)

	_, err := service.Acquire(t.Context(), workspace.ProjectID, workspace.IssueID, "runtime-missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Acquire() error=%v", err)
	}
	if implementation.createdSpec.RuntimeInstanceID != "" || implementation.startCalls != 0 {
		t.Fatalf("missing runtime must not provision compute: startCalls=%d created=%+v", implementation.startCalls, implementation.createdSpec)
	}
}
