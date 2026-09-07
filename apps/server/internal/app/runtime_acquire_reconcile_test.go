package app

import (
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRuntimeInstanceServiceAcquireReconcilesInterruptedProvisioning(t *testing.T) {
	projectID := "project-1"
	workspace := store.Workspace{ID: "workspace-1", ProjectID: projectID, IssueID: "issue-1", Path: "/workspaces/one", BootstrapStatus: "READY"}
	runtimeConfig := store.Runtime{ID: "runtime-1", ProjectID: &projectID, Kind: "docker", Image: "runtime:test", NetworkPolicy: "none", WorkspacePolicy: "issue", Enabled: true}
	base := &runtimeServiceStore{runtime: runtimeConfig, instance: store.RuntimeInstance{
		ID: "instance-interrupted", ProjectID: projectID, WorkspaceID: workspace.ID, RuntimeID: runtimeConfig.ID,
		Status: string(runtimepkg.StateProvisioning), RunnerStatus: "CONNECTING",
	}}
	rs := &reconcileStore{runtimeServiceStore: base, workspace: workspace}
	implementation := &recoveringRuntime{}
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
	if implementation.recoverCalls != 1 || implementation.createdSpec.RuntimeInstanceID != "" {
		t.Fatalf("recoverCalls=%d created=%+v", implementation.recoverCalls, implementation.createdSpec)
	}
}
