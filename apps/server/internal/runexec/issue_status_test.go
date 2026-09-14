package runexec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type recordingIssueStatusStore struct {
	mutation store.IssueStatusMutation
	err      error
}

func (s *recordingIssueStatusStore) SetIssueStatus(_ context.Context, mutation store.IssueStatusMutation) (store.IssueMutationResult, error) {
	s.mutation = mutation
	return store.IssueMutationResult{}, s.err
}

func TestIssueStatusUpdaterBindsMutationToActiveRun(t *testing.T) {
	agentID := "agent-1"
	statusStore := &recordingIssueStatusStore{}
	updater := &issueStatusUpdater{store: statusStore, safe: executioncontext.SafeContext{
		Project:   executioncontext.ProjectContext{ID: "project-1"},
		Issue:     executioncontext.IssueContext{ID: "issue-1"},
		Run:       executioncontext.RunContext{ID: "run-1"},
		Agent:     executioncontext.AgentContext{ID: agentID},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1"},
	}}

	if err := updater.SetStatus(context.Background(), "REVIEW"); err != nil {
		t.Fatalf("SetStatus() error=%v", err)
	}
	if statusStore.mutation.ProjectID != "project-1" || statusStore.mutation.IssueID != "issue-1" || statusStore.mutation.Status != "REVIEW" {
		t.Fatalf("mutation scope=%+v", statusStore.mutation)
	}
	if statusStore.mutation.RunID == nil || *statusStore.mutation.RunID != "run-1" || statusStore.mutation.AgentID == nil || *statusStore.mutation.AgentID != agentID || statusStore.mutation.WorkspaceID == nil || *statusStore.mutation.WorkspaceID != "workspace-1" {
		t.Fatalf("mutation provenance=%+v", statusStore.mutation)
	}
	var actor map[string]string
	if err := json.Unmarshal(statusStore.mutation.Actor, &actor); err != nil {
		t.Fatalf("decode actor: %v", err)
	}
	if actor["type"] != store.ActorTypeAgent || actor["id"] != agentID {
		t.Fatalf("actor=%v", actor)
	}
}

func TestIssueStatusUpdaterRejectsInvalidStatusAndPropagatesStoreFence(t *testing.T) {
	statusStore := &recordingIssueStatusStore{}
	updater := &issueStatusUpdater{store: statusStore, safe: executioncontext.SafeContext{
		Project:   executioncontext.ProjectContext{ID: "project-1"},
		Issue:     executioncontext.IssueContext{ID: "issue-1"},
		Run:       executioncontext.RunContext{ID: "run-1"},
		Agent:     executioncontext.AgentContext{ID: "agent-1"},
		Workspace: executioncontext.WorkspaceContext{ID: "workspace-1"},
	}}

	if err := updater.SetStatus(context.Background(), "NOT_A_STATUS"); err == nil {
		t.Fatal("invalid status unexpectedly accepted")
	}
	statusStore.err = store.ErrConflict
	if err := updater.SetStatus(context.Background(), "DONE"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("store fence error=%v want conflict", err)
	}
}
