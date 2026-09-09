package runexec

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

type workspaceBranchStore interface {
	GetWorkspace(context.Context, string, string) (store.Workspace, error)
	UpdateWorkspaceCurrentBranch(context.Context, string, string, string) (store.Workspace, error)
}

type branchObserver struct {
	store  workspaceBranchStore
	git    workspace.Git
	events *evidence.Recorder
}

func newBranchObserver(store workspaceBranchStore, git workspace.Git, events *evidence.Recorder) *branchObserver {
	if store == nil || git == nil || events == nil {
		return nil
	}
	return &branchObserver{store: store, git: git, events: events}
}

func persistedWorkspaceBranch(value store.Workspace) string {
	if value.CurrentBranch != nil && strings.TrimSpace(*value.CurrentBranch) != "" {
		return strings.TrimSpace(*value.CurrentBranch)
	}
	return strings.TrimSpace(value.WorkingBranch)
}

func (o *branchObserver) observeIfChanged(ctx context.Context, safe executioncontext.SafeContext, runtimeInstanceID *string) {
	if o == nil || strings.TrimSpace(safe.Workspace.Path) == "" || safe.Workspace.BootstrapStatus != "READY" {
		return
	}
	branch, err := o.git.CurrentBranch(ctx, safe.Workspace.Path)
	if err != nil {
		return
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return
	}
	workspace, err := o.store.GetWorkspace(ctx, safe.Project.ID, safe.Workspace.ID)
	if err != nil {
		return
	}
	previous := persistedWorkspaceBranch(workspace)
	if branch == previous {
		return
	}
	if _, err := o.store.UpdateWorkspaceCurrentBranch(ctx, safe.Project.ID, safe.Workspace.ID, branch); err != nil {
		return
	}
	payload := map[string]any{
		"branch":         branch,
		"previousBranch": previous,
		"detached":       strings.HasPrefix(branch, "HEAD@"),
		"issueKey":       safe.Issue.Key,
	}
	issueID, runID, agentID, workspaceID := safe.Issue.ID, safe.Run.ID, safe.Agent.ID, safe.Workspace.ID
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return
	}
	event := store.Event{
		Type:        "git.branch_checked_out",
		ProjectID:   safe.Project.ID,
		IssueID:     &issueID,
		RunID:       &runID,
		AgentID:     &agentID,
		WorkspaceID: &workspaceID,
		Actor:       store.EmptyObject,
		Payload:     encoded,
	}
	if runtimeInstanceID != nil && strings.TrimSpace(*runtimeInstanceID) != "" {
		event.RuntimeInstanceID = runtimeInstanceID
	}
	_, _ = o.events.Record(ctx, event)
}

func terminalToolEventTypes() map[string]struct{} {
	return map[string]struct{}{
		"tool.completed":  {},
		"tool.failed":     {},
		"tool.stopped":    {},
		"test.completed":  {},
		"test.failed":     {},
	}
}

func shouldObserveBranchAfterActivity(eventType string) bool {
	_, ok := terminalToolEventTypes()[eventType]
	return ok
}
