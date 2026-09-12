package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestExecutionSessionLifecycleAndSequentialRunnerReuse(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "execution-session-lifecycle")
	other := seedRunFixture(t, s, "execution-session-other")
	runnerRecord, err := s.CreateRunner(ctx, store.Runner{Name: "Build host", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	first, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: fixture.project.ID, RunID: fixture.run.ID, RunnerID: runnerRecord.ID,
		Status: "PENDING", CWD: "/workspace", CommandArgv: json.RawMessage(`["sh","-c","exit 7"]`),
	})
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}
	if first.RunnerID != runnerRecord.ID {
		t.Fatalf("runner binding=%q want=%q", first.RunnerID, runnerRecord.ID)
	}
	if _, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: fixture.project.ID, RunID: fixture.run.ID, RunnerID: runnerRecord.ID,
		Status: "PENDING", CommandArgv: json.RawMessage(`["true"]`),
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second active session error=%v, want ErrConflict", err)
	}

	first, err = s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: fixture.project.ID, SessionID: first.ID, FromStatuses: []string{"PENDING"}, Status: "STARTING",
		CommandArgv: json.RawMessage(`["opencode","serve"]`),
	})
	if err != nil || !bytes.Contains(first.CommandArgv, []byte("opencode")) {
		t.Fatalf("transition starting: session=%+v err=%v", first, err)
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
		ProjectID: fixture.project.ID, RunID: fixture.run.ID, RunnerID: runnerRecord.ID,
		Status: "PENDING", CommandArgv: json.RawMessage(`["true"]`),
	})
	if err != nil {
		t.Fatalf("sequential session should reuse Runner: %v", err)
	}
	active, err := s.ListExecutionSessionsByRunner(ctx, runnerRecord.ID, []string{"PENDING", "STARTING", "RUNNING"})
	if err != nil || len(active) != 1 || active[0].ID != second.ID {
		t.Fatalf("active sessions=%+v err=%v", active, err)
	}
	unfiltered, err := s.ListExecutionSessions(ctx, fixture.project.ID, []string{"PENDING", "COMPLETED"})
	if err != nil || len(unfiltered) != 2 {
		t.Fatalf("unfiltered sessions=%+v err=%v", unfiltered, err)
	}
	if _, err := s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: fixture.project.ID, SessionID: second.ID, FromStatuses: []string{"RUNNING"}, Status: "COMPLETED",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale transition error=%v, want ErrConflict", err)
	}
	if _, err := s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: fixture.project.ID, SessionID: second.ID, Status: "RUNNING",
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("empty transition precondition error=%v, want ErrInvalidArgument", err)
	}
	unchanged, err := s.GetExecutionSession(ctx, fixture.project.ID, second.ID)
	if err != nil || unchanged.Status != "PENDING" || unchanged.RunnerID != runnerRecord.ID {
		t.Fatalf("session after rejected transition=%+v err=%v", unchanged, err)
	}
	if _, err := s.GetExecutionSession(ctx, other.project.ID, second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project get error=%v", err)
	}
	if _, err := s.CreateExecutionSession(ctx, store.ExecutionSession{ProjectID: fixture.project.ID, RunID: fixture.run.ID}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("missing runner error=%v, want ErrInvalidArgument", err)
	}
	if _, err := s.ListExecutionSessions(ctx, fixture.project.ID, []string{"BOGUS"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid status error=%v", err)
	}
}

func TestListExecutionSessionsByRunner(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "runner-session-list")
	runnerRecord, err := s.CreateRunner(ctx, store.Runner{Name: "Build host", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}
	session, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: fixture.project.ID, RunID: fixture.run.ID, RunnerID: runnerRecord.ID,
		Status: "RUNNING", CommandArgv: json.RawMessage(`["true"]`),
	})
	if err != nil {
		t.Fatalf("create runner session: %v", err)
	}
	active, err := s.ListExecutionSessionsByRunner(ctx, runnerRecord.ID, []string{"RUNNING"})
	if err != nil || len(active) != 1 || active[0].ID != session.ID {
		t.Fatalf("active runner sessions=%+v err=%v", active, err)
	}
	if _, err := s.ListExecutionSessionsByRunner(ctx, "", nil); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("empty runner id error=%v", err)
	}
	if _, err := s.ListExecutionSessionsByRunner(ctx, runnerRecord.ID, []string{"BOGUS"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid status error=%v", err)
	}
}
