package executioncontext

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func resolveReviewFeedback(ctx context.Context, source Store, projectID, issueID string, run store.Run) (*ReviewFeedbackContext, error) {
	if run.Attempt <= 1 || !store.SupportsReviewStore(source) {
		return nil, nil
	}
	reviews := any(source).(store.ReviewStore)
	history, err := reviews.ListReviews(ctx, projectID, store.ReviewFilter{IssueID: &issueID, Statuses: []string{"CHANGES_REQUESTED"}})
	if err != nil {
		return nil, err
	}
	for index := len(history) - 1; index >= 0; index-- {
		review := history[index]
		previous, err := source.GetRun(ctx, projectID, review.RunID)
		if err != nil {
			return nil, err
		}
		if previous.IssueID != run.IssueID || previous.WorkspaceID != run.WorkspaceID || previous.Attempt != run.Attempt-1 {
			continue
		}
		if review.DecisionID == nil {
			return nil, fmt.Errorf("changes-requested Review %s has no Decision", review.ID)
		}
		decision, err := reviews.GetDecision(ctx, projectID, *review.DecisionID)
		if err != nil {
			return nil, err
		}
		if decision.Kind != "REVIEW" || decision.Outcome != "CHANGES_REQUESTED" || decision.RunID == nil || *decision.RunID != review.RunID {
			return nil, fmt.Errorf("changes-requested Review %s has an invalid Decision binding", review.ID)
		}
		var details struct {
			Feedback string `json:"feedback"`
		}
		if err := json.Unmarshal(decision.SafeDetails, &details); err != nil {
			return nil, fmt.Errorf("decode Review feedback: %w", err)
		}
		feedback := strings.TrimSpace(details.Feedback)
		if feedback == "" {
			return nil, fmt.Errorf("changes-requested Review %s has empty feedback", review.ID)
		}
		return &ReviewFeedbackContext{
			ReviewID:      review.ID,
			DecisionID:    decision.ID,
			PreviousRunID: review.RunID,
			Feedback:      feedback,
		}, nil
	}
	return nil, nil
}
