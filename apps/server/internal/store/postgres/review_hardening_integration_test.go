package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRuntimeInstanceStopTimeIsStable(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "stable-stop")

	instance, err := s.CreateRuntimeInstance(ctx, store.RuntimeInstance{
		ProjectID:   f.project.ID,
		WorkspaceID: f.workspace.ID,
		RuntimeID:   f.runtime.ID,
	})
	if err != nil {
		t.Fatalf("create runtime instance: %v", err)
	}

	failed, err := s.UpdateRuntimeInstanceState(ctx, f.project.ID, instance.ID, "FAILED", nil, "UNAVAILABLE", nil)
	if err != nil {
		t.Fatalf("mark runtime instance failed: %v", err)
	}
	if failed.StoppedAt == nil {
		t.Fatal("failed runtime instance did not record stopped_at")
	}

	time.Sleep(10 * time.Millisecond)
	stopped, err := s.UpdateRuntimeInstanceState(ctx, f.project.ID, instance.ID, "STOPPED", nil, "UNAVAILABLE", nil)
	if err != nil {
		t.Fatalf("mark runtime instance stopped: %v", err)
	}
	if stopped.StoppedAt == nil || !stopped.StoppedAt.Equal(*failed.StoppedAt) {
		t.Fatalf("stopped_at changed: first=%v second=%v", failed.StoppedAt, stopped.StoppedAt)
	}
}
