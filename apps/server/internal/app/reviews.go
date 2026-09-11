package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const reviewFailurePersistenceTimeout = 5 * time.Second

type ReviewTestStatus string

const (
	ReviewTestsNotRun  ReviewTestStatus = "NOT_RUN"
	ReviewTestsPassed  ReviewTestStatus = "PASSED"
	ReviewTestsFailed  ReviewTestStatus = "FAILED"
	ReviewTestsUnknown ReviewTestStatus = "UNKNOWN"
)

type ReviewInspection struct {
	Review     store.Review
	Decision   *store.Decision
	Evidence   RunEvidence
	TestStatus ReviewTestStatus
}

type reviewProjectStore interface {
	GetProject(context.Context, string) (store.Project, error)
}

type reviewRevisionApplier interface {
	ApplyReviewedRevision(context.Context, store.Project, store.Review) (string, error)
}

type ReviewService struct {
	store     store.ReviewStore
	projects  reviewProjectStore
	evidence  *RunEvidenceService
	applier   reviewRevisionApplier
	publisher persistedEventPublisher
}

func NewReviewService(reviewStore store.ReviewStore, projects reviewProjectStore, evidence *RunEvidenceService, applier reviewRevisionApplier) (*ReviewService, error) {
	if reviewStore == nil || projects == nil || evidence == nil || applier == nil {
		return nil, fmt.Errorf("review service dependencies are required")
	}
	return &ReviewService{store: reviewStore, projects: projects, evidence: evidence, applier: applier}, nil
}

func ReviewServiceFromServices(services *Services) *ReviewService {
	if services == nil || services.ExecutionStore == nil || services.RunEvidence == nil || services.Workspaces == nil || !store.SupportsReviewStore(services.ExecutionStore) {
		return nil
	}
	reviews := services.ExecutionStore.(store.ReviewStore)
	service, err := NewReviewService(reviews, services.ExecutionStore, services.RunEvidence, services.Workspaces)
	if err != nil {
		return nil
	}
	service.publisher = services.Events
	return service
}

func (s *ReviewService) List(ctx context.Context, projectID string, filter store.ReviewFilter) ([]store.Review, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, NewError("invalid_argument", "projectId is required", store.ErrInvalidArgument)
	}
	values, err := s.store.ListReviews(ctx, projectID, filter)
	if err != nil {
		return nil, translateStoreError(err, "review")
	}
	for index := range values {
		values[index], err = s.hydrateReviewRevisions(ctx, values[index])
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (s *ReviewService) Get(ctx context.Context, projectID, reviewID string) (ReviewInspection, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(reviewID) == "" {
		return ReviewInspection{}, NewError("invalid_argument", "projectId and reviewId are required", store.ErrInvalidArgument)
	}
	review, err := s.store.GetReview(ctx, projectID, reviewID)
	if err != nil {
		return ReviewInspection{}, translateStoreError(err, "review")
	}
	review, err = s.hydrateReviewRevisions(ctx, review)
	if err != nil {
		return ReviewInspection{}, err
	}
	evidence, err := s.evidence.Inspect(ctx, projectID, review.RunID)
	if err != nil {
		return ReviewInspection{}, err
	}
	inspection := ReviewInspection{Review: review, Evidence: evidence, TestStatus: summarizeReviewTests(evidence.Events)}
	if review.DecisionID == nil {
		return inspection, nil
	}
	decision, err := s.store.GetDecision(ctx, projectID, *review.DecisionID)
	if err != nil {
		return ReviewInspection{}, translateStoreError(err, "decision")
	}
	// Approval intent/failure rows are lifecycle bookkeeping. Only the final
	// attributable Review decision is part of the public Review result.
	if decision.Kind == "REVIEW" {
		inspection.Decision = &decision
	}
	return inspection, nil
}

func (s *ReviewService) Approve(ctx context.Context, projectID, reviewID string, actorID *string) (store.CompleteReviewApprovalResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(reviewID) == "" {
		return store.CompleteReviewApprovalResult{}, NewError("invalid_argument", "projectId and reviewId are required", store.ErrInvalidArgument)
	}
	begin, err := s.store.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: projectID, ReviewID: reviewID, ActorID: actorID})
	if err != nil {
		return store.CompleteReviewApprovalResult{}, translateStoreError(err, "review")
	}
	begin.Review, err = s.hydrateReviewRevisions(ctx, begin.Review)
	if err != nil {
		s.persistApprovalFailure(ctx, projectID, reviewID, "Review Git identity is unavailable")
		return store.CompleteReviewApprovalResult{}, err
	}
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return store.CompleteReviewApprovalResult{}, translateStoreError(err, "project")
	}

	sourceType := strings.TrimSpace(project.SourceType)
	if sourceType == "" {
		sourceType = store.ProjectSourceLocal
	}
	acceptedRevision := begin.Review.ReviewRevision
	deliveryComplete := false
	switch sourceType {
	case store.ProjectSourceLocal:
		acceptedRevision, err = s.applier.ApplyReviewedRevision(ctx, project, begin.Review)
		if err != nil {
			s.persistApprovalFailure(ctx, projectID, reviewID, "reviewed Git revision could not be integrated into the current Project Workspace")
			return store.CompleteReviewApprovalResult{}, err
		}
		deliveryComplete = true
	case store.ProjectSourceGit:
		// Publishing the Issue branch is not target integration. Approval pins the
		// accepted code identity but leaves Run/Issue delivery state unchanged.
	default:
		return store.CompleteReviewApprovalResult{}, NewError("review_delivery_invalid", "Project source type is not supported for Review delivery", store.ErrInvalidArgument)
	}

	result, err := s.store.CompleteReviewApproval(ctx, store.CompleteReviewApprovalCommand{
		ProjectID:        projectID,
		ReviewID:         reviewID,
		AcceptedRevision: acceptedRevision,
		DeliveryComplete: deliveryComplete,
	})
	if err != nil {
		// A local integration may already be durable when persistence fails.
		// The approval intent plus Git ancestry makes retry safe and idempotent.
		return store.CompleteReviewApprovalResult{}, translateStoreError(err, "review")
	}
	publishPersistedEvents(ctx, s.publisher, result.Events)
	return result, nil
}

func (s *ReviewService) RequestChanges(ctx context.Context, projectID, reviewID, feedback string, actorID *string) (store.RequestReviewChangesResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(reviewID) == "" {
		return store.RequestReviewChangesResult{}, NewError("invalid_argument", "projectId and reviewId are required", store.ErrInvalidArgument)
	}
	result, err := s.store.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{
		ProjectID: projectID,
		ReviewID:  reviewID,
		Feedback:  feedback,
		ActorID:   actorID,
	})
	if err != nil {
		return store.RequestReviewChangesResult{}, translateStoreError(err, "review")
	}
	publishPersistedEvents(ctx, s.publisher, result.Events)
	return result, nil
}

func (s *ReviewService) hydrateReviewRevisions(ctx context.Context, review store.Review) (store.Review, error) {
	if strings.TrimSpace(review.BaseRevision) != "" && strings.TrimSpace(review.ReviewRevision) != "" {
		return review, nil
	}
	revisions, ok := s.store.(store.ReviewRevisionStore)
	if !ok {
		return store.Review{}, NewError("review_evidence_invalid", "Review Git identity is unavailable", store.ErrConflict)
	}
	baseRevision, reviewRevision, err := revisions.GetReviewRevisions(ctx, review.ProjectID, review.ID)
	if err != nil {
		return store.Review{}, translateStoreError(err, "review")
	}
	review.BaseRevision = strings.TrimSpace(baseRevision)
	review.ReviewRevision = strings.TrimSpace(reviewRevision)
	if review.BaseRevision == "" || review.ReviewRevision == "" {
		return store.Review{}, NewError("review_evidence_invalid", "Review Git identity is unavailable", store.ErrConflict)
	}
	return review, nil
}

func (s *ReviewService) persistApprovalFailure(parent context.Context, projectID, reviewID, reason string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), reviewFailurePersistenceTimeout)
	defer cancel()
	_, _ = s.store.FailReviewApproval(ctx, store.FailReviewApprovalCommand{
		ProjectID: projectID,
		ReviewID:  reviewID,
		Reason:    reason,
	})
}

func summarizeReviewTests(events []store.Event) ReviewTestStatus {
	seen := false
	completed := false
	for _, event := range events {
		if !strings.HasPrefix(event.Type, "test.") {
			continue
		}
		seen = true
		switch event.Type {
		case "test.failed":
			return ReviewTestsFailed
		case "test.completed":
			completed = true
		}
	}
	if completed {
		return ReviewTestsPassed
	}
	if seen {
		return ReviewTestsUnknown
	}
	return ReviewTestsNotRun
}
