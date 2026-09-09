package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunnerGenerationRejectsSupersededConnectionStatus(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "runner-generation")
	instance, err := s.CreateRuntimeInstance(ctx, store.RuntimeInstance{
		ProjectID:   fixture.project.ID,
		WorkspaceID: fixture.workspace.ID,
		RuntimeID:   fixture.runtime.ID,
	})
	if err != nil {
		t.Fatalf("create Runtime Instance: %v", err)
	}
	externalID := "runner-generation-container"
	instance, err = s.UpdateRuntimeInstanceState(ctx, fixture.project.ID, instance.ID, "RUNNING", &externalID, "CONNECTING", json.RawMessage(`{"containerName":"runner-generation"}`))
	if err != nil {
		t.Fatalf("mark Runtime Instance running: %v", err)
	}

	first, err := s.ClaimRuntimeInstanceRunnerGeneration(ctx, fixture.project.ID, instance.ID)
	if err != nil || first != 1 {
		t.Fatalf("first generation=%d err=%v", first, err)
	}
	if _, err := s.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, fixture.project.ID, instance.ID, "READY", first); err != nil {
		t.Fatalf("persist first READY: %v", err)
	}

	second, err := s.ClaimRuntimeInstanceRunnerGeneration(ctx, fixture.project.ID, instance.ID)
	if err != nil || second != 2 {
		t.Fatalf("second generation=%d err=%v", second, err)
	}
	if _, err := s.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, fixture.project.ID, instance.ID, "READY", second); err != nil {
		t.Fatalf("persist second READY: %v", err)
	}
	if _, err := s.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, fixture.project.ID, instance.ID, "UNAVAILABLE", first); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale generation error=%v", err)
	}
	current, err := s.GetRuntimeInstance(ctx, fixture.project.ID, instance.ID)
	if err != nil || current.RunnerStatus != "READY" {
		t.Fatalf("current Runtime Instance=%+v err=%v", current, err)
	}

	if _, err := s.UpdateRuntimeInstanceRunnerStatus(ctx, fixture.project.ID, instance.ID, "BUSY"); err != nil {
		t.Fatalf("execution-owned BUSY update: %v", err)
	}
	if _, err := s.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, fixture.project.ID, instance.ID, "UNAVAILABLE", first); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale generation after unversioned update error=%v", err)
	}
	if _, err := s.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, fixture.project.ID, instance.ID, "READY", second); err != nil {
		t.Fatalf("current generation after unversioned update: %v", err)
	}
}

func TestRunnerGenerationRequiresRunningRuntimeInstanceAndValidToken(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "runner-generation-validation")
	instance, err := s.CreateRuntimeInstance(ctx, store.RuntimeInstance{
		ProjectID:   fixture.project.ID,
		WorkspaceID: fixture.workspace.ID,
		RuntimeID:   fixture.runtime.ID,
	})
	if err != nil {
		t.Fatalf("create Runtime Instance: %v", err)
	}
	if _, err := s.ClaimRuntimeInstanceRunnerGeneration(ctx, fixture.project.ID, instance.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("claim non-running Runtime Instance error=%v", err)
	}
	if _, err := s.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, fixture.project.ID, instance.ID, "READY", 0); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("zero generation error=%v", err)
	}
	if _, err := s.ClaimRuntimeInstanceRunnerGeneration(ctx, fixture.project.ID, "00000000-0000-0000-0000-000000000001"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing Runtime Instance error=%v", err)
	}
}
