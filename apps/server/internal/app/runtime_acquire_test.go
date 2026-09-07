package app

import (
	"context"
	"encoding/json"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type listingRuntimeStore struct{ *runtimeServiceStore }

func (s *listingRuntimeStore) ListRuntimeInstances(context.Context, string, []string) ([]store.RuntimeInstance, error) {
	if s.instance.ID == "" {
		return nil, nil
	}
	return []store.RuntimeInstance{s.instance}, nil
}

func TestRuntimeInstanceServiceAcquireRestartsStoppedWorkspaceRuntime(t *testing.T) {
	_, baseStore, implementation, workspace := runtimeServiceFixture(t)
	externalID := "container-1"
	baseStore.instance = store.RuntimeInstance{
		ID:                 "instance-existing",
		ProjectID:          workspace.ProjectID,
		WorkspaceID:        workspace.ID,
		RuntimeID:          baseStore.runtime.ID,
		Status:             "STOPPED",
		ExternalID:         &externalID,
		RunnerStatus:       "UNAVAILABLE",
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
	if instance.ID != "instance-existing" || instance.WorkspaceID != workspace.ID || instance.Status != "RUNNING" {
		t.Fatalf("acquired instance=%+v", instance)
	}
	if implementation.startCalls != 1 {
		t.Fatalf("start calls=%d want 1", implementation.startCalls)
	}
	if implementation.createdSpec.RuntimeInstanceID != "" {
		t.Fatalf("unexpected replacement Runtime creation: %+v", implementation.createdSpec)
	}
}
