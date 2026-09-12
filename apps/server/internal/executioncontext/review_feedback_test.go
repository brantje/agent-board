package executioncontext

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reviewFeedbackStore struct {
	runs      map[string]store.Run
	reviews   []store.Review
	decisions map[string]store.Decision
}

func (s *reviewFeedbackStore) GetProject(context.Context, string) (store.Project, error) {
	return store.Project{}, store.ErrNotFound
}
func (s *reviewFeedbackStore) GetIssue(context.Context, string, string) (store.Issue, error) {
	return store.Issue{}, store.ErrNotFound
}
func (s *reviewFeedbackStore) GetRun(_ context.Context, _, runID string) (store.Run, error) {
	value, ok := s.runs[runID]
	if !ok {
		return store.Run{}, store.ErrNotFound
	}
	return value, nil
}
func (s *reviewFeedbackStore) GetWorkspace(context.Context, string, string) (store.Workspace, error) {
	return store.Workspace{}, store.ErrNotFound
}
func (s *reviewFeedbackStore) GetAgentInScope(context.Context, *string, string) (store.Agent, error) {
	return store.Agent{}, store.ErrNotFound
}
func (s *reviewFeedbackStore) GetModelProfile(context.Context, *string, string) (store.ModelProfile, error) {
	return store.ModelProfile{}, store.ErrNotFound
}
func (s *reviewFeedbackStore) GetProvider(context.Context, *string, string) (store.Provider, error) {
	return store.Provider{}, store.ErrNotFound
}
func (s *reviewFeedbackStore) GetReview(context.Context, string, string) (store.Review, error) {
	return store.Review{}, store.ErrNotFound
}
func (s *reviewFeedbackStore) GetReviewByRun(context.Context, string, string) (store.Review, error) {
	return store.Review{}, store.ErrNotFound
}
func (s *reviewFeedbackStore) ListReviews(context.Context, string, store.ReviewFilter) ([]store.Review, error) {
	return append([]store.Review(nil), s.reviews...), nil
}
func (s *reviewFeedbackStore) GetDecision(_ context.Context, _ string, decisionID string) (store.Decision, error) {
	value, ok := s.decisions[decisionID]
	if !ok {
		return store.Decision{}, store.ErrNotFound
	}
	return value, nil
}
func (s *reviewFeedbackStore) BeginReviewApproval(context.Context, store.BeginReviewApprovalCommand) (store.BeginReviewApprovalResult, error) {
	return store.BeginReviewApprovalResult{}, nil
}
func (s *reviewFeedbackStore) CompleteReviewApproval(context.Context, store.CompleteReviewApprovalCommand) (store.CompleteReviewApprovalResult, error) {
	return store.CompleteReviewApprovalResult{}, nil
}
func (s *reviewFeedbackStore) FailReviewApproval(context.Context, store.FailReviewApprovalCommand) (store.Review, error) {
	return store.Review{}, nil
}
func (s *reviewFeedbackStore) RequestReviewChanges(context.Context, store.RequestReviewChangesCommand) (store.RequestReviewChangesResult, error) {
	return store.RequestReviewChangesResult{}, nil
}

func TestResolveReviewFeedbackUsesImmediatelyPreviousAttempt(t *testing.T) {
	previousRunID := "run-1"
	currentRun := store.Run{ID: "run-2", ProjectID: "project-1", IssueID: "issue-1", WorkspaceID: "workspace-1", Attempt: 2}
	decisionID := "decision-1"
	details, _ := json.Marshal(map[string]string{"feedback": "Please cover the restart case."})
	s := &reviewFeedbackStore{
		runs:      map[string]store.Run{previousRunID: {ID: previousRunID, ProjectID: currentRun.ProjectID, IssueID: currentRun.IssueID, WorkspaceID: currentRun.WorkspaceID, Attempt: 1}},
		reviews:   []store.Review{{ID: "review-1", ProjectID: currentRun.ProjectID, IssueID: currentRun.IssueID, RunID: previousRunID, Status: "CHANGES_REQUESTED", DecisionID: &decisionID}},
		decisions: map[string]store.Decision{decisionID: {ID: decisionID, ProjectID: currentRun.ProjectID, RunID: &previousRunID, Kind: "REVIEW", Outcome: "CHANGES_REQUESTED", SafeDetails: details}},
	}

	feedback, err := resolveReviewFeedback(context.Background(), s, currentRun.ProjectID, currentRun.IssueID, currentRun)
	if err != nil {
		t.Fatal(err)
	}
	if feedback == nil || feedback.ReviewID != "review-1" || feedback.DecisionID != decisionID || feedback.PreviousRunID != previousRunID || feedback.Feedback != "Please cover the restart case." {
		t.Fatalf("feedback=%+v", feedback)
	}
}

func TestResolveReviewFeedbackFirstAttemptHasNone(t *testing.T) {
	feedback, err := resolveReviewFeedback(context.Background(), &reviewFeedbackStore{}, "project-1", "issue-1", store.Run{Attempt: 1})
	if err != nil || feedback != nil {
		t.Fatalf("feedback=%+v err=%v", feedback, err)
	}
}
