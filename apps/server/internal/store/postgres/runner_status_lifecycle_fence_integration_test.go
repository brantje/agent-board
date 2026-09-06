package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunnerStatusLifecycleFenceRejectsStaleWrites(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "runner-status-lifecycle-fence")
	instance, err := s.CreateRuntimeInstance(ctx, store.RuntimeInstance{
		ProjectID:   fixture.project.ID,
		WorkspaceID: fixture.workspace.ID,
		RuntimeID:   fixture.runtime.ID,
	})
	if err != nil {
		t.Fatalf("create Runtime Instance: %v", err)
	}
	externalID := "runner-status-lifecycle-container"
	instance, err = s.UpdateRuntimeInstanceState(ctx, fixture.project.ID, instance.ID, "RUNNING", &externalID, "READY", json.RawMessage(`{"containerName":"runner-status-lifecycle"}`))
	if err != nil {
		t.Fatalf("mark Runtime Instance running: %v", err)
	}
	generation, err := s.ClaimRuntimeInstanceRunnerGeneration(ctx, fixture.project.ID, instance.ID)
	if err != nil {
		t.Fatalf("claim runner generation: %v", err)
	}
	instance, err = s.UpdateRuntimeInstanceState(ctx, fixture.project.ID, instance.ID, "STOPPING", &externalID, "DRAINING", instance.SafeHandleMetadata)
	if err != nil {
		t.Fatalf("move Runtime Instance to STOPPING: %v", err)
	}

	if _, err := s.UpdateRuntimeInstanceRunnerStatusIfStatus(ctx, fixture.project.ID, instance.ID, "READY", "RUNNING"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale lifecycle runner status error=%v", err)
	}
	if _, err := s.UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(ctx, fixture.project.ID, instance.ID, "UNAVAILABLE", generation, "RUNNING"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale lifecycle generation status error=%v", err)
	}
	current, err := s.GetRuntimeInstance(ctx, fixture.project.ID, instance.ID)
	if err != nil {
		t.Fatalf("get Runtime Instance: %v", err)
	}
	if current.Status != "STOPPING" || current.RunnerStatus != "DRAINING" {
		t.Fatalf("stale runner write mutated Runtime Instance: %+v", current)
	}
}
