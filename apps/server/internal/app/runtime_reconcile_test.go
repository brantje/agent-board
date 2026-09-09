package app

import (
	"context"
	"encoding/json"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reconcileStore struct {
	*runtimeServiceStore
	workspace store.Workspace
}

func (s *reconcileStore) ListProjects(context.Context) ([]store.Project, error) {
	return []store.Project{{ID: s.workspace.ProjectID}}, nil
}
func (s *reconcileStore) ListRuntimeInstances(_ context.Context, projectID string, statuses []string) ([]store.RuntimeInstance, error) {
	if s.instance.ID == "" || s.instance.ProjectID != projectID {
		return nil, nil
	}
	for _, status := range statuses {
		if status == s.instance.Status {
			return []store.RuntimeInstance{s.instance}, nil
		}
	}
	return nil, nil
}
func (s *reconcileStore) GetWorkspace(_ context.Context, projectID, workspaceID string) (store.Workspace, error) {
	if s.workspace.ProjectID != projectID || s.workspace.ID != workspaceID {
		return store.Workspace{}, store.ErrNotFound
	}
	return s.workspace, nil
}

type recoveringRuntime struct {
	fakeRuntimeImplementation
	recoverCalls int
}

func (r *recoveringRuntime) Recover(context.Context, runtimepkg.RuntimeSpec) (runtimepkg.Handle, runtimepkg.Inspection, error) {
	r.recoverCalls++
	return runtimepkg.Handle{ExternalID: "recovered-container", Metadata: json.RawMessage(`{"safe":true}`)}, runtimepkg.Inspection{ExternalID: "recovered-container", State: runtimepkg.StateRunning}, nil
}

func TestRuntimeInstanceReconcileRecoversMissingExternalIdentity(t *testing.T) {
	projectID := "project-1"
	workspace := store.Workspace{ID: "workspace-1", ProjectID: projectID, IssueID: "issue-1", Path: "/workspaces/one", BootstrapStatus: "READY"}
	runtimeConfig := store.Runtime{ID: "runtime-1", ProjectID: &projectID, Kind: "docker", Image: "runtime:test", NetworkPolicy: "none", WorkspacePolicy: "issue", Enabled: false}
	base := &runtimeServiceStore{runtime: runtimeConfig, instance: store.RuntimeInstance{ID: "instance-1", ProjectID: projectID, WorkspaceID: workspace.ID, RuntimeID: runtimeConfig.ID, Status: "PROVISIONING", RunnerStatus: "CONNECTING"}}
	rs := &reconcileStore{runtimeServiceStore: base, workspace: workspace}
	implementation := &recoveringRuntime{}
	service, err := NewRuntimeInstanceService(rs, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}

	instance, err := service.Reconcile(context.Background(), projectID, base.instance.ID)
	if err != nil || instance.Status != "RUNNING" || instance.ExternalID == nil || *instance.ExternalID != "recovered-container" || implementation.recoverCalls != 1 {
		t.Fatalf("Reconcile() instance=%+v recoverCalls=%d err=%v", instance, implementation.recoverCalls, err)
	}
}

func TestRuntimeInstanceReconcileAllUsesPersistedHandle(t *testing.T) {
	service, base, implementation, workspace := runtimeServiceFixture(t)
	externalID := "container-1"
	base.instance = store.RuntimeInstance{ID: "instance-1", ProjectID: workspace.ProjectID, WorkspaceID: workspace.ID, RuntimeID: base.runtime.ID, Status: "RUNNING", ExternalID: &externalID, SafeHandleMetadata: json.RawMessage(`{"safe":true}`)}
	rs := &reconcileStore{runtimeServiceStore: base, workspace: workspace}
	service.store = rs
	implementation.inspectState = runtimepkg.StateStopped

	if err := service.ReconcileAll(context.Background()); err != nil {
		t.Fatalf("ReconcileAll() error=%v", err)
	}
	if base.instance.Status != "STOPPED" || base.instance.RunnerStatus != "UNAVAILABLE" {
		t.Fatalf("reconciled instance=%+v", base.instance)
	}
}
