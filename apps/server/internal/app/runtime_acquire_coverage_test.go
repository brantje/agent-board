package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type failingListingRuntimeStore struct {
	*runtimeServiceStore
	err error
}

func (s *failingListingRuntimeStore) ListRuntimeInstances(context.Context, string, []string) ([]store.RuntimeInstance, error) {
	return nil, s.err
}

func TestRuntimeInstanceServiceAcquireReusesRunningWorkspaceRuntime(t *testing.T) {
	_, baseStore, implementation, workspace := runtimeServiceFixture(t)
	externalID := "container-existing"
	baseStore.instance = store.RuntimeInstance{
		ID:                 "instance-running",
		ProjectID:          workspace.ProjectID,
		WorkspaceID:        workspace.ID,
		RuntimeID:          baseStore.runtime.ID,
		Status:             "RUNNING",
		ExternalID:         &externalID,
		RunnerStatus:       "READY",
		SafeHandleMetadata: json.RawMessage(`{"safe":true}`),
	}
	service, err := NewRuntimeInstanceService(&listingRuntimeStore{runtimeServiceStore: baseStore}, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}

	instance, err := service.Acquire(context.Background(), workspace.ProjectID, workspace.IssueID, baseStore.runtime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if instance.ID != "instance-running" || instance.Status != "RUNNING" || instance.WorkspaceID != workspace.ID {
		t.Fatalf("Acquire()=%+v", instance)
	}
	if implementation.startCalls != 0 || implementation.createdSpec.RuntimeInstanceID != "" {
		t.Fatalf("unexpected lifecycle work start=%d created=%+v", implementation.startCalls, implementation.createdSpec)
	}
}

func TestRuntimeInstanceServiceAcquireCreatesReplacementWhenNoReusableRuntime(t *testing.T) {
	_, baseStore, implementation, workspace := runtimeServiceFixture(t)
	service, err := NewRuntimeInstanceService(&listingRuntimeStore{runtimeServiceStore: baseStore}, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}

	instance, err := service.Acquire(context.Background(), workspace.ProjectID, workspace.IssueID, baseStore.runtime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if instance.ID != "instance-1" || instance.WorkspaceID != workspace.ID || instance.Status != "PROVISIONING" {
		t.Fatalf("Acquire() replacement=%+v", instance)
	}
	if implementation.createdSpec.RuntimeInstanceID != instance.ID || implementation.createdSpec.WorkspaceID != workspace.ID {
		t.Fatalf("created spec=%+v", implementation.createdSpec)
	}
}

func TestRuntimeInstanceServiceAcquireValidatesAndTranslatesFailures(t *testing.T) {
	service, baseStore, implementation, workspace := runtimeServiceFixture(t)
	if _, err := service.Acquire(context.Background(), "", workspace.IssueID, baseStore.runtime.ID); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank project error=%v", err)
	}

	service.workspaces = &runtimeWorkspaceEnsurer{workspace: store.Workspace{ID: workspace.ID, ProjectID: workspace.ProjectID, IssueID: "other", BootstrapStatus: "READY"}}
	if _, err := service.Acquire(context.Background(), workspace.ProjectID, workspace.IssueID, baseStore.runtime.ID); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("workspace mismatch error=%v", err)
	}

	service, err := NewRuntimeInstanceService(&failingListingRuntimeStore{runtimeServiceStore: baseStore, err: store.ErrNotFound}, &runtimeWorkspaceEnsurer{workspace: workspace}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Acquire(context.Background(), workspace.ProjectID, workspace.IssueID, baseStore.runtime.ID); err == nil {
		t.Fatal("expected listing failure")
	} else if appErr, ok := AsError(err); !ok || appErr.Code != "runtime_instance_not_found" {
		t.Fatalf("translated listing error=%v", err)
	}
}
