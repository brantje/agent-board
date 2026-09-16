package evidence

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueStatusCapabilityStore struct {
	store.ControlPlaneStore
	mutation       store.IssueStatusMutation
	issueMutation  store.Issue
	placement      store.IssuePlacement
	placementActor json.RawMessage
	actor          json.RawMessage
	result         store.IssueMutationResult
}

func (s *issueStatusCapabilityStore) SetIssueStatus(_ context.Context, mutation store.IssueStatusMutation) (store.IssueMutationResult, error) {
	s.mutation = mutation
	return s.result, nil
}

func (s *issueStatusCapabilityStore) UpdateIssueMutationWithActor(_ context.Context, issue store.Issue, actor json.RawMessage) (store.IssueMutationResult, error) {
	s.issueMutation = issue
	s.actor = append(json.RawMessage(nil), actor...)
	return s.result, nil
}

func (s *issueStatusCapabilityStore) PlaceIssue(_ context.Context, placement store.IssuePlacement, actor json.RawMessage) (store.IssueMutationResult, error) {
	s.placement = placement
	s.placementActor = append(json.RawMessage(nil), actor...)
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

func TestRedactingStorePreservesActorAwareIssueMutationCapability(t *testing.T) {
	base := &issueStatusCapabilityStore{result: store.IssueMutationResult{Issue: store.Issue{ID: "issue-1", ProjectID: "project-1", Status: "REVIEW"}}}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())
	issue := store.Issue{ID: "issue-1", ProjectID: "project-1", Title: "Updated", Status: "REVIEW"}
	actor := json.RawMessage(`{"type":"HUMAN","id":"user-1"}`)

	result, err := wrapped.UpdateIssueMutationWithActor(t.Context(), issue, actor)
	if err != nil {
		t.Fatalf("UpdateIssueMutationWithActor() error=%v", err)
	}
	if result.Issue.Status != "REVIEW" || base.issueMutation.ID != issue.ID || base.issueMutation.Title != issue.Title || string(base.actor) != string(actor) {
		t.Fatalf("result=%+v issue=%+v actor=%s", result, base.issueMutation, base.actor)
	}
}

func TestRedactingStorePreservesIssuePlacementCapability(t *testing.T) {
	status := "TODO"
	beforeID := "issue-before"
	base := &issueStatusCapabilityStore{result: store.IssueMutationResult{Issue: store.Issue{ID: "issue-1", ProjectID: "project-1", Status: status}}}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())
	placement := store.IssuePlacement{
		ProjectID: "project-1",
		IssueID:   "issue-1",
		Status:    &status,
		BeforeID:  &beforeID,
		AfterID:   nil,
	}
	actor := json.RawMessage(`{"type":"HUMAN","id":"user-1"}`)

	result, err := wrapped.PlaceIssue(t.Context(), placement, actor)
	if err != nil {
		t.Fatalf("PlaceIssue() error=%v", err)
	}
	if result.Issue.Status != status || base.placement.ProjectID != placement.ProjectID || base.placement.IssueID != placement.IssueID || string(base.placementActor) != string(actor) {
		t.Fatalf("result=%+v placement=%+v actor=%s", result, base.placement, base.placementActor)
	}
	if base.placement.Status == nil || *base.placement.Status != status || base.placement.BeforeID == nil || *base.placement.BeforeID != beforeID || base.placement.AfterID != nil {
		t.Fatalf("placement=%+v", base.placement)
	}
}

func TestRedactingStoreReportsMissingIssueMutationCapabilities(t *testing.T) {
	wrapped := NewRedactingStore(&captureStore{}, redaction.NewRegistry())
	if _, err := wrapped.SetIssueStatus(t.Context(), store.IssueStatusMutation{ProjectID: "project", IssueID: "issue", Status: "IN_PROGRESS"}); err == nil {
		t.Fatal("expected missing Issue status mutation capability error")
	}
	if _, err := wrapped.UpdateIssueMutationWithActor(t.Context(), store.Issue{ProjectID: "project", ID: "issue", Status: "IN_PROGRESS"}, json.RawMessage(`{"type":"HUMAN"}`)); err == nil {
		t.Fatal("expected missing actor-aware Issue mutation capability error")
	}
	if _, err := wrapped.PlaceIssue(t.Context(), store.IssuePlacement{ProjectID: "project", IssueID: "issue"}, json.RawMessage(`{"type":"HUMAN"}`)); err == nil {
		t.Fatal("expected missing Issue placement capability error")
	}
}
