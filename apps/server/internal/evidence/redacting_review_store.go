package evidence

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *RedactingStore) SupportsReviewStore() bool {
	return store.SupportsReviewStore(s.ControlPlaneStore)
}

func (s *RedactingStore) reviewStore() (store.ReviewStore, error) {
	base, ok := s.ControlPlaneStore.(store.ReviewStore)
	if !ok || !store.SupportsReviewStore(base) {
		return nil, fmt.Errorf("redacting store base does not support Review operations")
	}
	return base, nil
}

func (s *RedactingStore) GetReview(ctx context.Context, projectID, reviewID string) (store.Review, error) {
	base, err := s.reviewStore()
	if err != nil {
		return store.Review{}, err
	}
	return base.GetReview(ctx, projectID, reviewID)
}

func (s *RedactingStore) GetReviewByRun(ctx context.Context, projectID, runID string) (store.Review, error) {
	base, err := s.reviewStore()
	if err != nil {
		return store.Review{}, err
	}
	return base.GetReviewByRun(ctx, projectID, runID)
}

func (s *RedactingStore) ListReviews(ctx context.Context, projectID string, filter store.ReviewFilter) ([]store.Review, error) {
	base, err := s.reviewStore()
	if err != nil {
		return nil, err
	}
	return base.ListReviews(ctx, projectID, filter)
}

func (s *RedactingStore) GetDecision(ctx context.Context, projectID, decisionID string) (store.Decision, error) {
	base, err := s.reviewStore()
	if err != nil {
		return store.Decision{}, err
	}
	return base.GetDecision(ctx, projectID, decisionID)
}

func (s *RedactingStore) BeginReviewApproval(ctx context.Context, command store.BeginReviewApprovalCommand) (store.BeginReviewApprovalResult, error) {
	base, err := s.reviewStore()
	if err != nil {
		return store.BeginReviewApprovalResult{}, err
	}
	return base.BeginReviewApproval(ctx, command)
}

func (s *RedactingStore) CompleteReviewApproval(ctx context.Context, command store.CompleteReviewApprovalCommand) (store.CompleteReviewApprovalResult, error) {
	base, err := s.reviewStore()
	if err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	return base.CompleteReviewApproval(ctx, command)
}

func (s *RedactingStore) FailReviewApproval(ctx context.Context, command store.FailReviewApprovalCommand) (store.Review, error) {
	base, err := s.reviewStore()
	if err != nil {
		return store.Review{}, err
	}
	return base.FailReviewApproval(ctx, command)
}

func (s *RedactingStore) RequestReviewChanges(ctx context.Context, command store.RequestReviewChangesCommand) (store.RequestReviewChangesResult, error) {
	base, err := s.reviewStore()
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	return base.RequestReviewChanges(ctx, command)
}
