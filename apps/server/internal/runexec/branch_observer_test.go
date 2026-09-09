package runexec

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type branchObserverStore struct {
	workspace store.Workspace
	updated   string
}

func (s *branchObserverStore) GetWorkspace(_ context.Context, projectID, workspaceID string) (store.Workspace, error) {
	if s.workspace.ProjectID != projectID || s.workspace.ID != workspaceID {
		return store.Workspace{}, store.ErrNotFound
	}
	return s.workspace, nil
}

func (s *branchObserverStore) UpdateWorkspaceCurrentBranch(_ context.Context, projectID, workspaceID, currentBranch string) (store.Workspace, error) {
	if s.workspace.ProjectID != projectID || s.workspace.ID != workspaceID {
		return store.Workspace{}, store.ErrNotFound
	}
	s.updated = currentBranch
	branch := currentBranch
	s.workspace.CurrentBranch = &branch
	return s.workspace, nil
}

type staticBranchGit struct {
	branch string
	err    error
}

func (g staticBranchGit) ValidateBranch(context.Context, string) error { return nil }
func (g staticBranchGit) Clone(context.Context, string, string, string) error {
	return nil
}
func (g staticBranchGit) InitRepository(context.Context, string, string) error { return nil }
func (g staticBranchGit) CheckoutNewBranch(context.Context, string, string) error {
	return nil
}
func (g staticBranchGit) HeadRevision(context.Context, string) (string, error) { return "", nil }
func (g staticBranchGit) CurrentBranch(context.Context, string) (string, error) {
	return g.branch, g.err
}
func (g staticBranchGit) OriginURL(context.Context, string) (string, error) { return "", nil }
func (g staticBranchGit) IsRepository(context.Context, string) (bool, error) {
	return true, nil
}

func TestBranchObserverRecordsCheckoutEvent(t *testing.T) {
	evidenceStore := &processTestStore{}
	hub := evidence.NewHub()
	recorder, err := evidence.NewRecorder(evidenceStore, hub)
	if err != nil {
		t.Fatal(err)
	}
	current := "agent-board/AB-12"
	state := &branchObserverStore{workspace: store.Workspace{
		ID: "workspace-1", ProjectID: "project-1", WorkingBranch: "agent-board/AB-12", CurrentBranch: &current, BootstrapStatus: "READY",
	}}
	observer := newBranchObserver(state, staticBranchGit{branch: "feat/foo"}, recorder)
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Issue:   executioncontext.IssueContext{ID: "issue-uuid", Key: "AB-12"},
		Run:     executioncontext.RunContext{ID: "run-1"},
		Agent:   executioncontext.AgentContext{ID: "agent-1"},
		Workspace: executioncontext.WorkspaceContext{
			ID: "workspace-1", Path: "/tmp/workspace", BootstrapStatus: "READY", WorkingBranch: "agent-board/AB-12",
		},
	}
	observer.observeIfChanged(context.Background(), safe, nil)
	if state.updated != "feat/foo" {
		t.Fatalf("updated branch=%q", state.updated)
	}
	if len(evidenceStore.events) != 1 || evidenceStore.events[0].Type != "git.branch_checked_out" {
		t.Fatalf("events=%+v", evidenceStore.events)
	}
	var payload map[string]any
	if err := json.Unmarshal(evidenceStore.events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["branch"] != "feat/foo" || payload["previousBranch"] != "agent-board/AB-12" || payload["issueKey"] != "AB-12" {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestBranchObserverSkipsUnchangedBranch(t *testing.T) {
	evidenceStore := &processTestStore{}
	recorder, err := evidence.NewRecorder(evidenceStore, evidence.NewHub())
	if err != nil {
		t.Fatal(err)
	}
	current := "agent-board/AB-12"
	state := &branchObserverStore{workspace: store.Workspace{
		ID: "workspace-1", ProjectID: "project-1", WorkingBranch: "agent-board/AB-12", CurrentBranch: &current, BootstrapStatus: "READY",
	}}
	observer := newBranchObserver(state, staticBranchGit{branch: "agent-board/AB-12"}, recorder)
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Issue:   executioncontext.IssueContext{ID: "issue-uuid", Key: "AB-12"},
		Run:     executioncontext.RunContext{ID: "run-1"},
		Agent:   executioncontext.AgentContext{ID: "agent-1"},
		Workspace: executioncontext.WorkspaceContext{
			ID: "workspace-1", Path: "/tmp/workspace", BootstrapStatus: "READY", WorkingBranch: "agent-board/AB-12",
		},
	}
	observer.observeIfChanged(context.Background(), safe, nil)
	if state.updated != "" || len(evidenceStore.events) != 0 {
		t.Fatalf("updated=%q events=%+v", state.updated, evidenceStore.events)
	}
}
