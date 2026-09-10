package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestExecutionSessionOwnsWorkspaceWriterUntilTerminal(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "workspace-writer")

	secondRun, err := s.CreateRun(ctx, store.Run{
		ProjectID: f.project.ID,
		IssueID: f.issue.ID,
		WorkspaceID: f.workspace.ID,
		AgentID: &f.agent.ID,
		Attempt: 2,
	})
	if err != nil {
		t.Fatalf("create second run: %v", err)
	}
	firstRunner, err := s.CreateRunner(ctx, store.Runner{Name: "workspace-writer-first", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatalf("create first runner: %v", err)
	}
	secondRunner, err := s.CreateRunner(ctx, store.Runner{Name: "workspace-writer-second", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatalf("create second runner: %v", err)
	}

	first, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID: f.run.ID,
		RunnerID: firstRunner.ID,
		Status: "PENDING",
	})
	if err != nil {
		t.Fatalf("create first writer: %v", err)
	}
	if _, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID: secondRun.ID,
		RunnerID: secondRunner.ID,
		Status: "PENDING",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("concurrent workspace writer error=%v, want ErrConflict", err)
	}

	if _, err := s.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: f.project.ID,
		SessionID: first.ID,
		FromStatuses: []string{"PENDING"},
		Status: "FAILED",
	}); err != nil {
		t.Fatalf("release first writer: %v", err)
	}
	if _, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID: secondRun.ID,
		RunnerID: secondRunner.ID,
		Status: "PENDING",
	}); err != nil {
		t.Fatalf("writer ownership was not released at terminal state: %v", err)
	}
}
