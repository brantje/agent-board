package runexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type serverFinalizeStore struct {
	processTestStore
	current   string
	persisted string
	updateErr error
	mismatch  bool
}

func (s *serverFinalizeStore) GetWorkspaceCurrentRevision(context.Context, string, string) (string, error) {
	return s.current, nil
}

func (s *serverFinalizeStore) UpdateWorkspaceCurrentRevision(_ context.Context, _, _, revision string) (string, error) {
	if s.updateErr != nil {
		return "", s.updateErr
	}
	s.persisted = strings.TrimSpace(revision)
	if s.mismatch {
		return "different", nil
	}
	return s.persisted, nil
}

type serverFinalizeGit struct {
	branch      string
	start       string
	revision    string
	finalizeErr error
}

func (g *serverFinalizeGit) ValidateBranch(context.Context, string) error { return nil }
func (g *serverFinalizeGit) Clone(context.Context, string, string, string) error { return nil }
func (g *serverFinalizeGit) InitRepository(context.Context, string, string) error { return nil }
func (g *serverFinalizeGit) CheckoutNewBranch(context.Context, string, string) error { return nil }
func (g *serverFinalizeGit) HeadRevision(context.Context, string) (string, error) { return g.revision, nil }
func (g *serverFinalizeGit) CurrentBranch(context.Context, string) (string, error) { return "agent-board/AB-1", nil }
func (g *serverFinalizeGit) OriginURL(context.Context, string) (string, error) { return "", nil }
func (g *serverFinalizeGit) IsRepository(context.Context, string) (bool, error) { return true, nil }
func (g *serverFinalizeGit) FinalizeCheckout(_ context.Context, _ string, branch, start string) (string, error) {
	g.branch = branch
	g.start = start
	if g.finalizeErr != nil {
		return "", g.finalizeErr
	}
	return g.revision, nil
}

func serverFinalizeSafeContext(base *string) executioncontext.SafeContext {
	return executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Workspace: executioncontext.WorkspaceContext{
			ID: "workspace-1", Path: "/workspace", BaseRevision: base, WorkingBranch: "agent-board/AB-1",
		},
	}
}

func TestFinalizeServerWorkspaceUsesPersistedStartRevision(t *testing.T) {
	storeFake := &serverFinalizeStore{current: "start-sha"}
	git := &serverFinalizeGit{revision: "review-sha"}
	processor := &Processor{store: storeFake, git: git}

	revision, err := processor.finalizeServerWorkspace(t.Context(), serverFinalizeSafeContext(nil))
	if err != nil {
		t.Fatal(err)
	}
	if revision != "review-sha" || git.branch != "agent-board/AB-1" || git.start != "start-sha" || storeFake.persisted != "review-sha" {
		t.Fatalf("revision=%q branch=%q start=%q persisted=%q", revision, git.branch, git.start, storeFake.persisted)
	}
}

func TestFinalizeServerWorkspaceFallsBackToBaseRevision(t *testing.T) {
	base := "base-sha"
	storeFake := &serverFinalizeStore{}
	git := &serverFinalizeGit{revision: "base-sha"}
	processor := &Processor{store: storeFake, git: git}

	if _, err := processor.finalizeServerWorkspace(t.Context(), serverFinalizeSafeContext(&base)); err != nil {
		t.Fatal(err)
	}
	if git.branch != "agent-board/AB-1" || git.start != base || storeFake.persisted != "base-sha" {
		t.Fatalf("branch=%q start=%q persisted=%q", git.branch, git.start, storeFake.persisted)
	}
}

func TestFinalizeServerWorkspaceRejectsUnsafePersistence(t *testing.T) {
	base := "base-sha"
	safe := serverFinalizeSafeContext(&base)

	t.Run("finalization failure does not persist", func(t *testing.T) {
		storeFake := &serverFinalizeStore{}
		processor := &Processor{
			store: storeFake,
			git:   &serverFinalizeGit{finalizeErr: errors.New("workspace branch changed")},
		}
		if _, err := processor.finalizeServerWorkspace(t.Context(), safe); err == nil {
			t.Fatal("unsafe checkout was finalized")
		}
		if storeFake.persisted != "" {
			t.Fatalf("rejected finalization persisted revision %q", storeFake.persisted)
		}
	})

	t.Run("revision persistence failure", func(t *testing.T) {
		processor := &Processor{
			store: &serverFinalizeStore{updateErr: errors.New("database unavailable")},
			git:   &serverFinalizeGit{revision: "review-sha"},
		}
		if _, err := processor.finalizeServerWorkspace(t.Context(), safe); err == nil {
			t.Fatal("persistence failure became success")
		}
	})

	t.Run("persisted revision mismatch", func(t *testing.T) {
		processor := &Processor{
			store: &serverFinalizeStore{mismatch: true},
			git:   &serverFinalizeGit{revision: "review-sha"},
		}
		if _, err := processor.finalizeServerWorkspace(t.Context(), safe); err == nil {
			t.Fatal("revision mismatch became success")
		}
	})
}

var _ store.WorkspaceRevisionStore = (*serverFinalizeStore)(nil)
