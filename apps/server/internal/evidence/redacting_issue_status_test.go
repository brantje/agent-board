package evidence

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueStatusCapabilityStore struct {
	store.ControlPlaneStore
	mutation store.IssueStatusMutation
	result   store.IssueMutationResult
}

func (s *issueStatusCapabilityStore) SetIssueStatus(_ context.Context, mutation store.IssueStatusMutation) (store.IssueMutationResult, error) {
	s.mutation = mutation
	return s.result, nil
}

func TestRedactingStorePreservesIssueStatusMutationCapability(t *testing.T) {
	runID, agentID, workspaceID := "run-1", "agent-1", "workspace-1"
	base := &issueStatusCapabilityStore{result: store.IssueMutationResult{Issue: store.Issue{ID: "issue-1", ProjectID: "project-1", Status: "IN_PROGRESS"}}}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())
	mutation := store.IssueStatusMutation{
		ProjectID: "project-1", IssueID: "issue-1", Status: "IN_PROGRESS",
		RunID: &runID, AgentID: &agentID, WorkspaceID: &workspaceID,
	}

	result, err := wrapped.SetIssueStatus(t.Context(), mutation)
	if err != nil {
		t.Fatalf("SetIssueStatus() error=%v", err)
	}
	if result.Issue.Status != "IN_PROGRESS" || base.mutation.ProjectID != mutation.ProjectID || base.mutation.IssueID != mutation.IssueID || base.mutation.Status != mutation.Status {
		t.Fatalf("result=%+v mutation=%+v", result, base.mutation)
	}
	if base.mutation.RunID == nil || *base.mutation.RunID != runID || base.mutation.AgentID == nil || *base.mutation.AgentID != agentID || base.mutation.WorkspaceID == nil || *base.mutation.WorkspaceID != workspaceID {
		t.Fatalf("provenance mutation=%+v", base.mutation)
	}
}

func TestRedactingStoreReportsMissingIssueStatusMutationCapability(t *testing.T) {
	wrapped := NewRedactingStore(&captureStore{}, redaction.NewRegistry())
	if _, err := wrapped.SetIssueStatus(t.Context(), store.IssueStatusMutation{ProjectID: "project", IssueID: "issue", Status: "IN_PROGRESS"}); err == nil {
		t.Fatal("expected missing Issue status mutation capability error")
	}
}
