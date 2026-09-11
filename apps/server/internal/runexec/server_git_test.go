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
	start     string
	revision  string
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
func (g *serverFinalizeGit) FinalizeCheckout(_ context.Context, _ string, start string) (string, error) {
	g.start = start
	if g.finalizeErr != nil {
		return "", g.finalizeErr
	}
	return g.revision, nil
}

func TestFinalizeServerWorkspaceUsesPersistedStartRevision(t *testing.T) {
	storeFake := &serverFinalizeStore{current: "start-sha"}
	git := &serverFinalizeGit{revision: "review-sha"}
	processor := &Processor{store: storeFake, git: git}
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1", Path: "/workspace"},
	}

	revision, err := processor.finalizeServerWorkspace(t.Context(), safe)
	if err != nil {
		t.Fatal(err)
	}
	if revision != "review-sha" || git.start != "start-sha" || storeFake.persisted != "review-sha" {
		t.Fatalf("revision=%q start=%q persisted=%q", revision, git.start, storeFake.persisted)
	}
}

func TestFinalizeServerWorkspaceFallsBackToBaseRevision(t *testing.T) {
	base := "base-sha"
	storeFake := &serverFinalizeStore{}
	git := &serverFinalizeGit{revision: "base-sha"}
	processor := &Processor{store: storeFake, git: git}
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1", Path: "/workspace", BaseRevision: &base},
	}

	if _, err := processor.finalizeServerWorkspace(t.Context(), safe); err != nil {
		t.Fatal(err)
	}
	if git.start != base || storeFake.persisted != "base-sha" {
		t.Fatalf("start=%q persisted=%q", git.start, storeFake.persisted)
	}
}

func TestFinalizeServerWorkspaceRejectsUnsafePersistence(t *testing.T) {
	base := "base-sha"
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1", Path: "/workspace", BaseRevision: &base},
	}

	t.Run("finalization failure", func(t *testing.T) {
		processor := &Processor{
			store: &serverFinalizeStore{},
			git:   &serverFinalizeGit{finalizeErr: errors.New("history rewritten")},
		}
		if _, err := processor.finalizeServerWorkspace(t.Context(), safe); err == nil {
			t.Fatal("unsafe checkout was finalized")
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
