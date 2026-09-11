package postgres

import (
	"context"
	"errors"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"testing"
)

func TestProjectRunnerAttachmentsAreScopedAndExternalOnly(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	pid := insertProject(t, s.pool, "runner-project")
	external, err := s.CreateRunner(ctx, store.Runner{Name: "External", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	internal, err := s.CreateRunner(ctx, store.Runner{Name: "Internal", Internal: true, TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := s.ListProjectRunnerIDs(ctx, pid)
	if err != nil || len(ids) != 0 {
		t.Fatal("default allowlist not empty")
	}
	if err = s.SetProjectRunnerIDs(ctx, pid, []string{external.ID}); err != nil {
		t.Fatal(err)
	}
	ids, err = s.ListProjectRunnerIDs(ctx, pid)
	if err != nil || len(ids) != 1 || ids[0] != external.ID {
		t.Fatal("attachment lost")
	}
	if err = s.SetProjectRunnerIDs(ctx, pid, []string{internal.ID}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("internal attachment accepted: %v", err)
	}
	ids, _ = s.ListProjectRunnerIDs(ctx, pid)
	if len(ids) != 1 || ids[0] != external.ID {
		t.Fatal("failed replacement lost policy")
	}
	if err = s.SetProjectRunnerIDs(ctx, pid, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ListProjectRunnerIDs(ctx, "11111111-1111-4111-8111-111111111111"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project: %v", err)
	}
}

func TestProjectInternalRunnerPolicyDefaultsAndUpdates(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	p, err := s.CreateProject(ctx, testProjectInput("policy", "/repo", "RP"))
	if err != nil {
		t.Fatal(err)
	}
	if p.AllowInternalRunner == nil || !*p.AllowInternalRunner {
		t.Fatal("internal fallback must default true")
	}
	disabled := false
	p.AllowInternalRunner = &disabled
	if _, err = s.UpdateProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	p, err = s.GetProject(ctx, p.ID)
	if err != nil || p.AllowInternalRunner == nil || *p.AllowInternalRunner {
		t.Fatal("internal opt-out lost")
	}
}
