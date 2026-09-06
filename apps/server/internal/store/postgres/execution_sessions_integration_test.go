package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestExecutionSessionLifecycleAndSequentialReuse(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "execution-session-lifecycle")
	other := seedRunFixture(t, s, "execution-session-other")

	instance, err := s.CreateRuntimeInstance(ctx, store.RuntimeInstance{
		ProjectID: fixture.project.ID, WorkspaceID: fixture.workspace.ID, RuntimeID: fixture.runtime.ID,
	})
	if err != nil {
		t.Fatalf("create runtime instance: %v", err)
	}

	first, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: fixture.project.ID, RunID: fixture.run.ID, RuntimeInstanceID: instance.ID,
		Status: "PENDING", CWD: "/workspace", CommandArgv: json.RawMessage(`["sh","-c","exit 7"]`),
	})
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}
	if _, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: fixture.project.ID, RunID: fixture.run.ID, RuntimeInstanceID: instance.ID,
		Status: "PENDING", CommandArgv: json.RawMessage(`["true"]`),
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second active session error=%v, want ErrConflict", err)
	}

	first, err = s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: fixture.project.ID, SessionID: first.ID, FromStatuses: []string{"PENDING"}, Status: "STARTING",
	})
	if err != nil {
		t.Fatalf("transition starting: %v", err)
	}
	first, err = s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: fixture.project.ID, SessionID: first.ID, FromStatuses: []string{"STARTING"}, Status: "RUNNING",
	})
	if err != nil || first.StartedAt == nil {
		t.Fatalf("transition running: session=%+v err=%v", first, err)
	}
	exitCode := 7
	first, err = s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: fixture.project.ID, SessionID: first.ID, FromStatuses: []string{"RUNNING"}, Status: "COMPLETED", ExitCode: &exitCode,
	})
	if err != nil || first.CompletedAt == nil || first.ExitCode == nil || *first.ExitCode != exitCode {
		t.Fatalf("transition completed: session=%+v err=%v", first, err)
	}

	second, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: fixture.project.ID, RunID: fixture.run.ID, RuntimeInstanceID: instance.ID,
		Status: "PENDING", CommandArgv: json.RawMessage(`["true"]`),
	})
	if err != nil {
		t.Fatalf("sequential session should reuse Runtime Instance: %v", err)
	}
	active, err := s.ListExecutionSessionsByRuntimeInstance(ctx, fixture.project.ID, instance.ID, []string{"PENDING", "STARTING", "RUNNING"})
	if err != nil || len(active) != 1 || active[0].ID != second.ID {
		t.Fatalf("active sessions=%+v err=%v", active, err)
	}
	if _, err := s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: fixture.project.ID, SessionID: second.ID, FromStatuses: []string{"RUNNING"}, Status: "COMPLETED",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale transition error=%v, want ErrConflict", err)
	}
	if _, err := s.GetExecutionSession(ctx, other.project.ID, second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project get error=%v", err)
	}
	if _, err := s.ListExecutionSessions(ctx, fixture.project.ID, []string{"BOGUS"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid status error=%v", err)
	}
}
