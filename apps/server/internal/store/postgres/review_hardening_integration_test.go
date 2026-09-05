package postgres

import (
	"context"
	"errors"
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

func TestSchedulerIdempotencyKeyCollisionReturnsConflict(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	first := seedRunFixture(t, s, "idem-first")
	second := seedRunFixture(t, s, "idem-second")

	if _, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID: first.project.ID,
		RunID: first.run.ID,
		Kind: "START",
		IdempotencyKey: "shared-key",
	}); err != nil {
		t.Fatalf("enqueue first job: %v", err)
	}

	if _, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID: second.project.ID,
		RunID: second.run.ID,
		Kind: "START",
		IdempotencyKey: "shared-key",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-job collision error=%v, want ErrConflict", err)
	}

	if _, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID: first.project.ID,
		RunID: first.run.ID,
		Kind: "RESUME",
		IdempotencyKey: "shared-key",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("kind collision error=%v, want ErrConflict", err)
	}
}
