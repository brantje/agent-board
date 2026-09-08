package executioncontext

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestResolveReviewFeedbackHandlesMissingAndMismatchedHistory(t *testing.T) {
	current := store.Run{ID: "run-2", ProjectID: "project-1", IssueID: "issue-1", WorkspaceID: "workspace-1", Attempt: 2}

	t.Run("no changes-requested history", func(t *testing.T) {
		feedback, err := resolveReviewFeedback(t.Context(), &reviewFeedbackStore{}, current.ProjectID, current.IssueID, current)
		if err != nil || feedback != nil {
			t.Fatalf("feedback=%+v err=%v", feedback, err)
		}
	})

	t.Run("missing previous run", func(t *testing.T) {
		s := &reviewFeedbackStore{reviews: []store.Review{{ID: "review", RunID: "missing"}}}
		if _, err := resolveReviewFeedback(t.Context(), s, current.ProjectID, current.IssueID, current); err == nil {
			t.Fatal("missing previous Run should fail")
		}
	})

	t.Run("unrelated history is ignored", func(t *testing.T) {
		previous := store.Run{ID: "run-1", ProjectID: current.ProjectID, IssueID: "other-issue", WorkspaceID: current.WorkspaceID, Attempt: 1}
		s := &reviewFeedbackStore{
			runs:    map[string]store.Run{previous.ID: previous},
			reviews: []store.Review{{ID: "review", RunID: previous.ID}},
		}
		feedback, err := resolveReviewFeedback(t.Context(), s, current.ProjectID, current.IssueID, current)
		if err != nil || feedback != nil {
			t.Fatalf("feedback=%+v err=%v", feedback, err)
		}
	})
}

func TestResolveReviewFeedbackRejectsInvalidDecisionBindings(t *testing.T) {
	previousID := "run-1"
	current := store.Run{ID: "run-2", ProjectID: "project-1", IssueID: "issue-1", WorkspaceID: "workspace-1", Attempt: 2}
	previous := store.Run{ID: previousID, ProjectID: current.ProjectID, IssueID: current.IssueID, WorkspaceID: current.WorkspaceID, Attempt: 1}
	decisionID := "decision-1"
	baseReview := store.Review{ID: "review-1", ProjectID: current.ProjectID, IssueID: current.IssueID, RunID: previousID, Status: "CHANGES_REQUESTED", DecisionID: &decisionID}
	validDetails, _ := json.Marshal(map[string]string{"feedback": "fix it"})
	baseDecision := store.Decision{ID: decisionID, ProjectID: current.ProjectID, RunID: &previousID, Kind: "REVIEW", Outcome: "CHANGES_REQUESTED", SafeDetails: validDetails}

	cases := []struct {
		name     string
		review   store.Review
		decision *store.Decision
		want     string
	}{
		{name: "missing decision id", review: func() store.Review { value := baseReview; value.DecisionID = nil; return value }(), want: "no Decision"},
		{name: "missing decision", review: baseReview, decision: nil, want: ""},
		{name: "wrong kind", review: baseReview, decision: func() *store.Decision { value := baseDecision; value.Kind = "QUESTION"; return &value }(), want: "invalid Decision binding"},
		{name: "wrong outcome", review: baseReview, decision: func() *store.Decision { value := baseDecision; value.Outcome = "APPROVED"; return &value }(), want: "invalid Decision binding"},
		{name: "missing run binding", review: baseReview, decision: func() *store.Decision { value := baseDecision; value.RunID = nil; return &value }(), want: "invalid Decision binding"},
		{name: "wrong run binding", review: baseReview, decision: func() *store.Decision {
			value := baseDecision
			other := "other-run"
			value.RunID = &other
			return &value
		}(), want: "invalid Decision binding"},
		{name: "malformed details", review: baseReview, decision: func() *store.Decision { value := baseDecision; value.SafeDetails = json.RawMessage(`{`); return &value }(), want: "decode Review feedback"},
		{name: "empty feedback", review: baseReview, decision: func() *store.Decision {
			value := baseDecision
			value.SafeDetails = json.RawMessage(`{"feedback":"  "}`)
			return &value
		}(), want: "empty feedback"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &reviewFeedbackStore{
				runs:      map[string]store.Run{previousID: previous},
				reviews:   []store.Review{tc.review},
				decisions: map[string]store.Decision{},
			}
			if tc.decision != nil {
				s.decisions[decisionID] = *tc.decision
			}
			_, err := resolveReviewFeedback(t.Context(), s, current.ProjectID, current.IssueID, current)
			if err == nil {
				t.Fatal("expected invalid review history to fail")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want substring %q", err, tc.want)
			}
		})
	}
}

// noReviewCapabilityStore satisfies the execution-context Store contract but
// deliberately does not expose ReviewStore. Attempts after the first must then
// remain valid without synthetic review feedback.
type noReviewCapabilityStore struct{ Store }

func TestResolveReviewFeedbackWithoutReviewCapability(t *testing.T) {
	feedback, err := resolveReviewFeedback(context.Background(), &noReviewCapabilityStore{}, "project", "issue", store.Run{Attempt: 2})
	if err != nil || feedback != nil {
		t.Fatalf("feedback=%+v err=%v", feedback, err)
	}
}
