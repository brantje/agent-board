package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunScopedExecutionEvidenceQueries(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "run-scoped-evidence")
	other := seedRunFixture(t, s, "run-scoped-evidence-other")

	createSession := func(t *testing.T, f runFixture) store.ExecutionSession {
		t.Helper()
		instance, err := s.CreateRuntimeInstance(ctx, store.RuntimeInstance{
			ProjectID: f.project.ID, WorkspaceID: f.workspace.ID, RuntimeID: f.runtime.ID,
		})
		if err != nil {
			t.Fatalf("create runtime instance: %v", err)
		}
		session, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
			ProjectID: f.project.ID, RunID: f.run.ID, RuntimeInstanceID: instance.ID,
			Status: "PENDING", CWD: "/workspace", CommandArgv: json.RawMessage(`["true"]`),
		})
		if err != nil {
			t.Fatalf("create execution session: %v", err)
		}
		return session
	}

	want := createSession(t, fixture)
	_ = createSession(t, other)

	sessions, err := s.ListExecutionSessionsByRun(ctx, fixture.project.ID, fixture.run.ID, nil)
	if err != nil || len(sessions) != 1 || sessions[0].ID != want.ID {
		t.Fatalf("run sessions=%+v err=%v", sessions, err)
	}
	pending, err := s.ListExecutionSessionsByRun(ctx, fixture.project.ID, fixture.run.ID, []string{"PENDING"})
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending run sessions=%+v err=%v", pending, err)
	}
	completed, err := s.ListExecutionSessionsByRun(ctx, fixture.project.ID, fixture.run.ID, []string{"COMPLETED"})
	if err != nil || len(completed) != 0 {
		t.Fatalf("completed run sessions=%+v err=%v", completed, err)
	}
	if _, err := s.ListExecutionSessionsByRun(ctx, fixture.project.ID, "", nil); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("empty run id error=%v, want ErrInvalidArgument", err)
	}

	chunk, err := s.CreateRawOutputChunk(ctx, store.RawOutputChunk{
		ProjectID: fixture.project.ID, IssueID: fixture.issue.ID, RunID: fixture.run.ID,
		Stream: "STDOUT", Sequence: 1, StorageRef: "blob:test", SizeBytes: 4,
	})
	if err != nil {
		t.Fatalf("create raw output chunk: %v", err)
	}
	chunks, err := s.ListRawOutputChunks(ctx, fixture.project.ID, fixture.run.ID)
	if err != nil || len(chunks) != 1 || chunks[0].ID != chunk.ID {
		t.Fatalf("raw output chunks=%+v err=%v", chunks, err)
	}
	foreign, err := s.ListRawOutputChunks(ctx, other.project.ID, fixture.run.ID)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-project raw output=%+v err=%v", foreign, err)
	}
}
