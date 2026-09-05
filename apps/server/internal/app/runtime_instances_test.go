package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runtimeServiceStore struct {
	runtime  store.Runtime
	instance store.RuntimeInstance
	updates  []string
}

func (s *runtimeServiceStore) GetRuntime(_ context.Context, scope *string, id string) (store.Runtime, error) {
	if id != s.runtime.ID || (s.runtime.ProjectID != nil && (scope == nil || *scope != *s.runtime.ProjectID)) {
		return store.Runtime{}, store.ErrNotFound
	}
	return s.runtime, nil
}
func (s *runtimeServiceStore) CreateRuntimeInstance(_ context.Context, input store.RuntimeInstance) (store.RuntimeInstance, error) {
	input.ID = "instance-1"
	if input.Status == "" {
		input.Status = "PROVISIONING"
	}
	s.instance = input
	return input, nil
}
func (s *runtimeServiceStore) GetRuntimeInstance(_ context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	if s.instance.ID != instanceID || s.instance.ProjectID != projectID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	return s.instance, nil
}
func (s *runtimeServiceStore) UpdateRuntimeInstanceState(_ context.Context, projectID, instanceID, status string, externalID *string, runnerStatus string, metadata json.RawMessage) (store.RuntimeInstance, error) {
	if s.instance.ID != instanceID || s.instance.ProjectID != projectID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	s.instance.Status = status
	s.instance.ExternalID = externalID
	s.instance.RunnerStatus = runnerStatus
	s.instance.SafeHandleMetadata = metadata
	s.updates = append(s.updates, status)
	return s.instance, nil
}

type runtimeWorkspaceEnsurer struct {
	workspace store.Workspace
	err       error
}

func (w *runtimeWorkspaceEnsurer) EnsureIssueWorkspace(context.Context, string, string) (store.Workspace, error) {
	return w.workspace, w.err
}

type fakeRuntimeImplementation struct {
	createErr    error
	startErr     error
	inspectState runtimepkg.State
	stopErr      error
	destroyErr   error
	startCalls   int
	stopCalls    int
	destroyCalls int
	createdSpec  runtimepkg.RuntimeSpec
}

func (f *fakeRuntimeImplementation) Create(_ context.Context, spec runtimepkg.RuntimeSpec) (runtimepkg.Handle, error) {
	f.createdSpec = spec
	if f.createErr != nil {
		return runtimepkg.Handle{}, f.createErr
	}
	return runtimepkg.Handle{ExternalID: "container-1", Metadata: json.RawMessage(`{"safe":true}`)}, nil
}
func (f *fakeRuntimeImplementation) Start(context.Context, runtimepkg.Handle) error {
	f.startCalls++
	return f.startErr
}
func (f *fakeRuntimeImplementation) Inspect(context.Context, runtimepkg.Handle) (runtimepkg.Inspection, error) {
	state := f.inspectState
	if state == "" {
		state = runtimepkg.StateRunning
	}
	return runtimepkg.Inspection{ExternalID: "container-1", State: state}, nil
}
func (f *fakeRuntimeImplementation) Stop(context.Context, runtimepkg.Handle, runtimepkg.StopReason) error {
	f.stopCalls++
	return f.stopErr
}
func (f *fakeRuntimeImplementation) Destroy(context.Context, runtimepkg.Handle) error {
	f.destroyCalls++
	return f.destroyErr
}

func runtimeServiceFixture(t *testing.T) (*RuntimeInstanceService, *runtimeServiceStore, *fakeRuntimeImplementation, store.Workspace) {
	t.Helper()
	projectID := "project-1"
	workspace := store.Workspace{ID: "workspace-1", ProjectID: projectID, IssueID: "issue-1", Path: "/var/lib/agent-board/workspaces/workspace-1", BootstrapStatus: "READY"}
	runtimeConfig := store.Runtime{ID: "runtime-1", ProjectID: &projectID, Name: "Docker", Kind: "docker", Image: "runtime:test", NetworkPolicy: "none", WorkspacePolicy: "issue", Enabled: true}
	runtimeStore := &runtimeServiceStore{runtime: runtimeConfig}
	implementation := &fakeRuntimeImplementation{}
	service, err := NewRuntimeInstanceService(runtimeStore, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}
	return service, runtimeStore, implementation, workspace
}

func TestRuntimeInstanceServiceLifecyclePreservesWorkspaceBinding(t *testing.T) {
	service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
	ctx := context.Background()

	instance, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
	if err != nil {
		t.Fatalf("Create() error=%v", err)
	}
	if instance.Status != "PROVISIONING" || instance.WorkspaceID != workspace.ID || instance.ExternalID == nil || *instance.ExternalID != "container-1" {
		t.Fatalf("created instance=%+v", instance)
	}
	if implementation.createdSpec.Workspace.Source != workspace.Path || implementation.createdSpec.WorkspaceID != workspace.ID || implementation.createdSpec.IssueID != workspace.IssueID {
		t.Fatalf("RuntimeSpec=%+v", implementation.createdSpec)
	}

	instance, err = service.Start(ctx, workspace.ProjectID, instance.ID)
	if err != nil || instance.Status != "RUNNING" || implementation.startCalls != 1 {
		t.Fatalf("Start() instance=%+v calls=%d err=%v", instance, implementation.startCalls, err)
	}
	inspection, err := service.Inspect(ctx, workspace.ProjectID, instance.ID)
	if err != nil || inspection.State != runtimepkg.StateRunning {
		t.Fatalf("Inspect()=%+v err=%v", inspection, err)
	}
	instance, err = service.Stop(ctx, workspace.ProjectID, instance.ID, runtimepkg.StopReasonRequested)
	if err != nil || instance.Status != "STOPPED" || implementation.stopCalls != 1 {
		t.Fatalf("Stop() instance=%+v calls=%d err=%v", instance, implementation.stopCalls, err)
	}
	instance, err = service.Destroy(ctx, workspace.ProjectID, instance.ID)
	if err != nil || instance.Status != "DESTROYED" || implementation.destroyCalls != 1 {
		t.Fatalf("Destroy() instance=%+v calls=%d err=%v", instance, implementation.destroyCalls, err)
	}
	if instance.WorkspaceID != workspace.ID {
		t.Fatalf("workspace binding changed: %+v", instance)
	}
}

func TestRuntimeInstanceServiceDestroyRunningStopsFirst(t *testing.T) {
	service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
	ctx := context.Background()
	instance, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
	if err != nil {
		t.Fatal(err)
	}
	instance, err = service.Start(ctx, workspace.ProjectID, instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	instance, err = service.Destroy(ctx, workspace.ProjectID, instance.ID)
	if err != nil || instance.Status != "DESTROYED" || implementation.stopCalls != 1 || implementation.destroyCalls != 1 {
		t.Fatalf("Destroy() instance=%+v stop=%d destroy=%d err=%v", instance, implementation.stopCalls, implementation.destroyCalls, err)
	}
}

func TestRuntimeInstanceServicePersistsCreateFailure(t *testing.T) {
	service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
	implementation.createErr = errors.New("create failed")
	instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
	if err == nil || instance.Status != "FAILED" || runtimeStore.instance.Status != "FAILED" {
		t.Fatalf("Create() instance=%+v persisted=%+v err=%v", instance, runtimeStore.instance, err)
	}
}

func TestRuntimeInstanceServiceRejectsInvalidConfigurationAndUnreconciledHandles(t *testing.T) {
	service, runtimeStore, _, workspace := runtimeServiceFixture(t)
	ctx := context.Background()
	if _, err := service.Create(ctx, "", workspace.IssueID, runtimeStore.runtime.ID); err == nil {
		t.Fatal("expected invalid identity")
	}
	runtimeStore.runtime.Enabled = false
	if _, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID); err == nil {
		t.Fatal("expected disabled Runtime rejection")
	}
	runtimeStore.runtime.Enabled = true
	runtimeStore.instance = store.RuntimeInstance{ID: "instance-1", ProjectID: workspace.ProjectID, WorkspaceID: workspace.ID, RuntimeID: runtimeStore.runtime.ID, Status: "FAILED"}
	if _, err := service.Destroy(ctx, workspace.ProjectID, runtimeStore.instance.ID); !errors.Is(err, runtimepkg.ErrNotFound) {
		t.Fatalf("Destroy unreconciled error=%v", err)
	}
}

func TestRuntimeInstanceServiceRejectsWorkspaceMismatch(t *testing.T) {
	service, runtimeStore, _, workspace := runtimeServiceFixture(t)
	service.workspaces = &runtimeWorkspaceEnsurer{workspace: store.Workspace{ID: "workspace-1", ProjectID: workspace.ProjectID, IssueID: "other", Path: workspace.Path, BootstrapStatus: "READY"}}
	if _, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID); err == nil {
		t.Fatal("expected workspace mismatch")
	}
}
