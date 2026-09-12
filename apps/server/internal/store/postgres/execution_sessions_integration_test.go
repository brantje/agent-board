package postgres

import (
	"bytes"
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
	if err != nil || unchanged.Status != "PENDING" {
		t.Fatalf("session after rejected empty precondition=%+v err=%v", unchanged, err)
	}
	if _, err := s.GetExecutionSession(ctx, other.project.ID, second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project get error=%v", err)
	}
	if _, err := s.ListExecutionSessions(ctx, fixture.project.ID, []string{"BOGUS"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid status error=%v", err)
	}
}

func TestRunnerAllowsConcurrentExecutionSessionsOnDifferentWorkspaces(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	first := seedRunFixture(t, s, "runner-concurrent-first")
	secondRun := createQueuedFixtureRun(t, s, first, "runner-concurrent-second")
	runner, err := s.CreateRunner(ctx, store.Runner{Name: "shared-runner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	firstSession, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: first.project.ID, RunID: first.run.ID, RunnerID: runner.ID,
		Status: "RUNNING", CommandArgv: json.RawMessage(`["true"]`),
	})
	if err != nil {
		t.Fatalf("create first runner session: %v", err)
	}
	secondSession, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: first.project.ID, RunID: secondRun.ID, RunnerID: runner.ID,
		Status: "PENDING", CommandArgv: json.RawMessage(`["true"]`),
	})
	if err != nil {
		t.Fatalf("create second runner session on different workspace: %v", err)
	}
	active, err := s.ListExecutionSessionsByRunner(ctx, runner.ID, []string{"PENDING", "STARTING", "RUNNING"})
	if err != nil || len(active) != 2 {
		t.Fatalf("active runner sessions=%+v err=%v", active, err)
	}
	if active[0].ID != firstSession.ID && active[1].ID != firstSession.ID {
		t.Fatalf("first session missing from active set=%+v", active)
	}
	if active[0].ID != secondSession.ID && active[1].ID != secondSession.ID {
		t.Fatalf("second session missing from active set=%+v", active)
	}
}

func TestListExecutionSessionsByRunner(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "runner-session-list")
	runner, err := s.CreateRunner(ctx, store.Runner{Name: "Build host", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}
	session, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: fixture.project.ID, RunID: fixture.run.ID, RunnerID: runner.ID,
		Status: "RUNNING", CommandArgv: json.RawMessage(`["true"]`),
	})
	if err != nil {
		t.Fatalf("create runner session: %v", err)
	}
	active, err := s.ListExecutionSessionsByRunner(ctx, runner.ID, []string{"RUNNING"})
	if err != nil || len(active) != 1 || active[0].ID != session.ID {
		t.Fatalf("active runner sessions=%+v err=%v", active, err)
	}
	if _, err := s.ListExecutionSessionsByRunner(ctx, "", nil); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("empty runner id error=%v", err)
	}
	if _, err := s.ListExecutionSessionsByRunner(ctx, runner.ID, []string{"BOGUS"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid status error=%v", err)
	}
}
