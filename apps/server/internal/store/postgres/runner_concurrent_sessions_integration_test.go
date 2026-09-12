package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunnerOwnsMultipleConcurrentExecutionSessions(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "runner-concurrent")
	runner, err := s.CreateRunner(ctx, store.Runner{Name: "runner-concurrent", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	secondRun := createQueuedFixtureRun(t, s, f, "runner-concurrent-second")

	first, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID:     f.run.ID,
		RunnerID:  runner.ID,
		Status:    "PENDING",
	})
	if err != nil {
		t.Fatalf("create first runner session: %v", err)
	}
	second, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID:     secondRun.ID,
		RunnerID:  runner.ID,
		Status:    "PENDING",
	})
	if err != nil {
		t.Fatalf("create second runner session on same runner: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("expected distinct sessions, got %q", first.ID)
	}

	active, err := s.ListExecutionSessionsByRunner(ctx, runner.ID, []string{"PENDING", "STARTING", "RUNNING"})
	if err != nil {
		t.Fatalf("list active runner sessions: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("active runner sessions=%d want 2", len(active))
	}
}
