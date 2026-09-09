package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRuntimeInstanceRecoveryQueriesAreScoped(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "runtime-recovery")
	other := seedRunFixture(t, s, "runtime-recovery-other")

	instance, err := s.CreateRuntimeInstance(ctx, store.RuntimeInstance{
		ProjectID:   fixture.project.ID,
		WorkspaceID: fixture.workspace.ID,
		RuntimeID:   fixture.runtime.ID,
	})
	if err != nil {
		t.Fatalf("create runtime instance: %v", err)
	}

	got, err := s.GetRuntimeInstance(ctx, fixture.project.ID, instance.ID)
	if err != nil || got.WorkspaceID != fixture.workspace.ID || got.RuntimeID != fixture.runtime.ID {
		t.Fatalf("get runtime instance: got=%+v err=%v", got, err)
	}
	if _, err := s.GetRuntimeInstance(ctx, other.project.ID, instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project get error=%v", err)
	}

	externalID := "container-runtime-recovery"
	updated, err := s.UpdateRuntimeInstanceState(ctx, fixture.project.ID, instance.ID, "RUNNING", &externalID, "CONNECTING", json.RawMessage(`{"containerName":"agent-board-runtime-recovery"}`))
	if err != nil {
		t.Fatalf("update runtime instance: %v", err)
	}
	if updated.WorkspaceID != fixture.workspace.ID || updated.RuntimeID != fixture.runtime.ID || updated.ExternalID == nil || *updated.ExternalID != externalID {
		t.Fatalf("immutable binding or external identity changed: %+v", updated)
	}

	active, err := s.ListRuntimeInstances(ctx, fixture.project.ID, []string{"PROVISIONING", "STARTING", "RUNNING", "STOPPING"})
	if err != nil || len(active) != 1 || active[0].ID != instance.ID {
		t.Fatalf("list active: got=%+v err=%v", active, err)
	}
	foreign, err := s.ListRuntimeInstances(ctx, other.project.ID, []string{"RUNNING"})
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-project list: got=%+v err=%v", foreign, err)
	}

	updated, err = s.UpdateRuntimeInstanceState(ctx, fixture.project.ID, instance.ID, "STOPPED", &externalID, "UNAVAILABLE", updated.SafeHandleMetadata)
	if err != nil || updated.ExternalID == nil || *updated.ExternalID != externalID {
		t.Fatalf("stop runtime instance: got=%+v err=%v", updated, err)
	}
	active, err = s.ListRuntimeInstances(ctx, fixture.project.ID, []string{"PROVISIONING", "STARTING", "RUNNING", "STOPPING"})
	if err != nil || len(active) != 0 {
		t.Fatalf("stopped instance should not be active: got=%+v err=%v", active, err)
	}
	all, err := s.ListRuntimeInstances(ctx, fixture.project.ID, nil)
	if err != nil || len(all) != 1 || all[0].ID != instance.ID {
		t.Fatalf("list all: got=%+v err=%v", all, err)
	}
}

func TestRuntimeInstanceRecoveryQueriesValidateArguments(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	if _, err := s.GetRuntimeInstance(ctx, "", "missing"); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank project get error=%v", err)
	}
	if _, err := s.ListRuntimeInstances(ctx, "", nil); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank project list error=%v", err)
	}
	if _, err := s.ListRuntimeInstances(ctx, "00000000-0000-0000-0000-000000000001", []string{"BOGUS"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("bad status list error=%v", err)
	}
}
