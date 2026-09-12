package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestReviewReadsReturnDurableReviewAndDecision(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	fixture, pending := readyReviewFixture(t, s, "review-reads")

	review, err := s.GetReview(ctx, fixture.project.ID, pending.ID)
	if err != nil {
		t.Fatalf("GetReview() error=%v", err)
	}
	if review.ID != pending.ID || review.RunID != fixture.run.ID || review.Status != "PENDING" {
		t.Fatalf("GetReview()=%+v", review)
	}
	byRun, err := s.GetReviewByRun(ctx, fixture.project.ID, fixture.run.ID)
	if err != nil {
		t.Fatalf("GetReviewByRun() error=%v", err)
	}
	if byRun.ID != review.ID {
		t.Fatalf("GetReviewByRun()=%+v", byRun)
	}

	begin, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: fixture.project.ID, ReviewID: pending.ID})
	if err != nil {
		t.Fatalf("BeginReviewApproval() error=%v", err)
	}
	decision, err := s.GetDecision(ctx, fixture.project.ID, begin.Decision.ID)
	if err != nil {
		t.Fatalf("GetDecision() error=%v", err)
	}
	if decision.ID != begin.Decision.ID || decision.Kind != "REVIEW_APPROVAL_INTENT" || decision.Outcome != "PENDING" {
		t.Fatalf("GetDecision()=%+v", decision)
	}

	if _, err := s.GetReview(ctx, fixture.project.ID, "00000000-0000-4000-8000-000000000000"); err == nil {
		t.Fatal("missing Review should fail")
	}
	if _, err := s.GetDecision(ctx, fixture.project.ID, "00000000-0000-4000-8000-000000000000"); err == nil {
		t.Fatal("missing Decision should fail")
	}
}
