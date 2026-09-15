package runexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueStatusStore interface {
	store.IssueStatusMutationStore
}

type issueStatusUpdater struct {
	store  issueStatusStore
	events *evidence.Recorder
	safe   executioncontext.SafeContext
}

func (u *issueStatusUpdater) SetStatus(ctx context.Context, status string) error {
	return u.setStatus(ctx, status, false)
}

func (u *issueStatusUpdater) SetRecoveredStatus(ctx context.Context, status string) error {
	if err := u.setStatus(ctx, status, true); err != nil {
		if errors.Is(err, store.ErrIssueStatusRecoverySuperseded) {
			return nil
		}
		return err
	}
	return nil
}

func (u *issueStatusUpdater) setStatus(ctx context.Context, status string, recovery bool) error {
	if u == nil || u.store == nil {
		return fmt.Errorf("run execution: Issue status capability is unavailable")
	}
	if !store.ValidIssueStatus(status) {
		return fmt.Errorf("run execution: unsupported Issue status %q", status)
	}

	actor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": u.safe.Agent.ID})
	if err != nil {
		return err
	}
	runID, agentID, workspaceID := u.safe.Run.ID, u.safe.Agent.ID, u.safe.Workspace.ID
	result, err := u.store.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID:   u.safe.Project.ID,
		IssueID:     u.safe.Issue.ID,
		Status:      status,
		Actor:       actor,
		RunID:       &runID,
		AgentID:     &agentID,
		WorkspaceID: &workspaceID,
		Recovery:    recovery,
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
