package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runnerWorkspaceEnsurer struct {
	workspace store.Workspace
	err       error
}

func (e runnerWorkspaceEnsurer) EnsureIssueWorkspace(context.Context, string, string) (store.Workspace, error) {
	return e.workspace, e.err
}

func TestEnsureRunnerWorkspaceFailsClosedAcrossMaterializationBoundaries(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

	t.Run("already ready without materializer", func(t *testing.T) {
		processor := &Processor{}
		got, err := processor.ensureRunnerWorkspace(t.Context(), run, safe)
		if err != nil || got.Workspace.ID != safe.Workspace.ID {
			t.Fatalf("workspace=%+v err=%v", got.Workspace, err)
		}
	})

	t.Run("pending without materializer", func(t *testing.T) {
		processor := &Processor{}
		pending := safe
		pending.Workspace.Path = "pending://workspace"
		if _, err := processor.ensureRunnerWorkspace(t.Context(), run, pending); err == nil {
			t.Fatal("pending workspace was accepted")
		}
	})

	t.Run("materializer failure", func(t *testing.T) {
		want := errors.New("materialization failed")
		processor := &Processor{workspaces: runnerWorkspaceEnsurer{err: want}}
		if _, err := processor.ensureRunnerWorkspace(t.Context(), run, safe); !errors.Is(err, want) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("materializer returns unready workspace", func(t *testing.T) {
		processor := &Processor{workspaces: runnerWorkspaceEnsurer{workspace: store.Workspace{ID: safe.Workspace.ID, Path: "pending://workspace", BootstrapStatus: "PENDING"}}}
		if _, err := processor.ensureRunnerWorkspace(t.Context(), run, safe); err == nil {
			t.Fatal("unready materialized workspace was accepted")
		}
	})

	ready := store.Workspace{ID: safe.Workspace.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, Path: safe.Workspace.Path, BootstrapStatus: "READY"}

	t.Run("resolver failure", func(t *testing.T) {
		want := errors.New("resolve failed")
		processor := &Processor{workspaces: runnerWorkspaceEnsurer{workspace: ready}, resolver: processTestResolver{err: want}}
		if _, err := processor.ensureRunnerWorkspace(t.Context(), run, safe); !errors.Is(err, want) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("binding changes during materialization", func(t *testing.T) {
		changed := safe
		changed.Workspace.Path = safe.Workspace.Path + "-changed"
		processor := &Processor{
			workspaces: runnerWorkspaceEnsurer{workspace: ready},
			resolver:   processTestResolver{resolved: executioncontext.Resolved{Safe: changed}},
		}
		if _, err := processor.ensureRunnerWorkspace(t.Context(), run, safe); err == nil {
			t.Fatal("changed workspace binding was accepted")
		}
	})

	t.Run("stable materialization", func(t *testing.T) {
		processor := &Processor{
			workspaces: runnerWorkspaceEnsurer{workspace: ready},
			resolver:   processTestResolver{resolved: executioncontext.Resolved{Safe: safe}},
		}
		got, err := processor.ensureRunnerWorkspace(t.Context(), run, safe)
		if err != nil || got.Workspace.ID != safe.Workspace.ID || got.Workspace.Path != safe.Workspace.Path {
			t.Fatalf("workspace=%+v err=%v", got.Workspace, err)
		}
	})
}
