package evidence

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reviewForwardStore struct {
	store.ControlPlaneStore
	review   store.Review
	decision store.Decision
	begin    store.BeginReviewApprovalResult
	complete store.CompleteReviewApprovalResult
	request  store.RequestReviewChangesResult
	failed   store.Review
	calls    []string
}

func (s *reviewForwardStore) GetReview(context.Context, string, string) (store.Review, error) {
	s.calls = append(s.calls, "get")
	return s.review, nil
}
func (s *reviewForwardStore) GetReviewByRun(context.Context, string, string) (store.Review, error) {
	s.calls = append(s.calls, "get-by-run")
	return s.review, nil
}
func (s *reviewForwardStore) ListReviews(context.Context, string, store.ReviewFilter) ([]store.Review, error) {
	s.calls = append(s.calls, "list")
	return []store.Review{s.review}, nil
}
func (s *reviewForwardStore) GetDecision(context.Context, string, string) (store.Decision, error) {
	s.calls = append(s.calls, "decision")
	return s.decision, nil
}
func (s *reviewForwardStore) BeginReviewApproval(context.Context, store.BeginReviewApprovalCommand) (store.BeginReviewApprovalResult, error) {
	s.calls = append(s.calls, "begin")
	return s.begin, nil
}
func (s *reviewForwardStore) CompleteReviewApproval(context.Context, store.CompleteReviewApprovalCommand) (store.CompleteReviewApprovalResult, error) {
	s.calls = append(s.calls, "complete")
	return s.complete, nil
}
func (s *reviewForwardStore) FailReviewApproval(context.Context, store.FailReviewApprovalCommand) (store.Review, error) {
	s.calls = append(s.calls, "fail")
	return s.failed, nil
}
func (s *reviewForwardStore) RequestReviewChanges(context.Context, store.RequestReviewChangesCommand) (store.RequestReviewChangesResult, error) {
	s.calls = append(s.calls, "changes")
	return s.request, nil
}

func TestRedactingStorePreservesReviewCapabilityAndForwardsCommands(t *testing.T) {
	base := &reviewForwardStore{
		review:   store.Review{ID: "review", Status: "PENDING"},
		decision: store.Decision{ID: "decision", Outcome: "APPROVED"},
		begin:    store.BeginReviewApprovalResult{Review: store.Review{ID: "review"}},
		complete: store.CompleteReviewApprovalResult{Review: store.Review{ID: "review", Status: "APPROVED"}},
		request:  store.RequestReviewChangesResult{Review: store.Review{ID: "review", Status: "CHANGES_REQUESTED"}},
		failed:   store.Review{ID: "review", Status: "PENDING"},
	}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())
	ctx := t.Context()
	if !wrapped.SupportsReviewStore() {
		t.Fatal("review capability was not preserved")
	}
	if got, err := wrapped.GetReview(ctx, "project", "review"); err != nil || got.ID != "review" {
		t.Fatalf("GetReview()=%+v err=%v", got, err)
	}
	if got, err := wrapped.GetReviewByRun(ctx, "project", "run"); err != nil || got.ID != "review" {
		t.Fatalf("GetReviewByRun()=%+v err=%v", got, err)
	}
	if got, err := wrapped.ListReviews(ctx, "project", store.ReviewFilter{}); err != nil || len(got) != 1 {
		t.Fatalf("ListReviews()=%+v err=%v", got, err)
	}
	if got, err := wrapped.GetDecision(ctx, "project", "decision"); err != nil || got.ID != "decision" {
		t.Fatalf("GetDecision()=%+v err=%v", got, err)
	}
	if got, err := wrapped.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: "project", ReviewID: "review"}); err != nil || got.Review.ID != "review" {
		t.Fatalf("BeginReviewApproval()=%+v err=%v", got, err)
	}
	if got, err := wrapped.CompleteReviewApproval(ctx, store.CompleteReviewApprovalCommand{ProjectID: "project", ReviewID: "review", AcceptedRevision: "sha"}); err != nil || got.Review.Status != "APPROVED" {
		t.Fatalf("CompleteReviewApproval()=%+v err=%v", got, err)
	}
	if got, err := wrapped.FailReviewApproval(ctx, store.FailReviewApprovalCommand{ProjectID: "project", ReviewID: "review", Reason: "failed"}); err != nil || got.Status != "PENDING" {
		t.Fatalf("FailReviewApproval()=%+v err=%v", got, err)
	}
	if got, err := wrapped.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{ProjectID: "project", ReviewID: "review", Feedback: "fix"}); err != nil || got.Review.Status != "CHANGES_REQUESTED" {
		t.Fatalf("RequestReviewChanges()=%+v err=%v", got, err)
	}
	if len(base.calls) != 8 {
		t.Fatalf("forwarded calls=%v", base.calls)
	}
}

func TestRedactingStoreReviewMethodsRejectMissingCapability(t *testing.T) {
	wrapped := NewRedactingStore(&captureStore{}, redaction.NewRegistry())
	ctx := t.Context()
	if wrapped.SupportsReviewStore() {
		t.Fatal("unexpected review capability")
	}
	if _, err := wrapped.GetReview(ctx, "project", "review"); err == nil {
		t.Fatal("GetReview should reject missing capability")
	}
	if _, err := wrapped.ListReviews(ctx, "project", store.ReviewFilter{}); err == nil {
		t.Fatal("ListReviews should reject missing capability")
	}
	if _, err := wrapped.GetDecision(ctx, "project", "decision"); err == nil {
		t.Fatal("GetDecision should reject missing capability")
	}
	if _, err := wrapped.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{}); err == nil {
		t.Fatal("BeginReviewApproval should reject missing capability")
	}
	if _, err := wrapped.CompleteReviewApproval(ctx, store.CompleteReviewApprovalCommand{}); err == nil {
		t.Fatal("CompleteReviewApproval should reject missing capability")
	}
	if _, err := wrapped.FailReviewApproval(ctx, store.FailReviewApprovalCommand{}); err == nil {
		t.Fatal("FailReviewApproval should reject missing capability")
	}
	if _, err := wrapped.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{}); err == nil {
		t.Fatal("RequestReviewChanges should reject missing capability")
	}
}
