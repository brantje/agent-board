package runexec

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueStatusStore interface {
	store.IssueStatusMutationStore
	GetRun(context.Context, string, string) (store.Run, error)
}

type issueStatusUpdater struct {
	store  issueStatusStore
	events *evidence.Recorder
	safe   executioncontext.SafeContext
}

func (u *issueStatusUpdater) SetStatus(ctx context.Context, status string) error {
	if u == nil || u.store == nil {
		return fmt.Errorf("run execution: Issue status capability is unavailable")
	}
	if !store.ValidIssueStatus(status) {
		return fmt.Errorf("run execution: unsupported Issue status %q", status)
	}

	run, err := u.store.GetRun(ctx, u.safe.Project.ID, u.safe.Run.ID)
	if err != nil {
		return err
	}
	if run.Status != "RUNNING" || run.ProjectID != u.safe.Project.ID || run.IssueID != u.safe.Issue.ID ||
		run.WorkspaceID != u.safe.Workspace.ID || run.AgentID == nil || *run.AgentID != u.safe.Agent.ID {
		return fmt.Errorf("run execution: Issue status capability is no longer bound to the active Run")
	}

	actor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": u.safe.Agent.ID})
	if err != nil {
		return err
	}
	runID, agentID, workspaceID := run.ID, u.safe.Agent.ID, run.WorkspaceID
	result, err := u.store.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID:   u.safe.Project.ID,
		IssueID:     u.safe.Issue.ID,
		Status:      status,
		Actor:       actor,
		RunID:       &runID,
		AgentID:     &agentID,
		WorkspaceID: &workspaceID,
	})
	if err != nil {
		return err
	}
	if u.events != nil {
		for _, event := range result.Events {
			u.events.PublishPersisted(ctx, event)
		}
	}
	return nil
}
