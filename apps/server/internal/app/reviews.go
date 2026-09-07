package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	evidencepkg "github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
	workspacepkg "github.com/brantje/agent-board/apps/server/internal/workspace"
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

type reviewCandidateApplier interface {
	ApplyReviewedCandidate(context.Context, store.Project, string, workspacepkg.AcceptedCandidate) (string, error)
}

type ReviewService struct {
	store      store.ReviewStore
	projects   reviewProjectStore
	evidence   *RunEvidenceService
	candidates evidencepkg.ReviewCandidateReader
	applier    reviewCandidateApplier
}

// NewReviewService builds the Review application boundary. Production callers
// should pass the private candidate reader explicitly. Tests and composite
// stores may omit it only when the ReviewStore itself implements that reader.
func NewReviewService(reviewStore store.ReviewStore, projects reviewProjectStore, evidence *RunEvidenceService, applier reviewCandidateApplier, candidateReaders ...evidencepkg.ReviewCandidateReader) (*ReviewService, error) {
	if reviewStore == nil || projects == nil || evidence == nil || applier == nil || len(candidateReaders) > 1 {
		return nil, fmt.Errorf("review service dependencies are required")
	}
	var candidates evidencepkg.ReviewCandidateReader
	if len(candidateReaders) == 1 {
		candidates = candidateReaders[0]
	} else if reader, ok := any(reviewStore).(evidencepkg.ReviewCandidateReader); ok {
		candidates = reader
	}
	if candidates == nil {
		return nil, fmt.Errorf("review candidate reader is required")
	}
	return &ReviewService{store: reviewStore, projects: projects, evidence: evidence, candidates: candidates, applier: applier}, nil
}

// ReviewServiceFromServices exposes Review only when the configured persistence,
// public evidence and private candidate-delivery stacks are all available.
func ReviewServiceFromServices(services *Services) *ReviewService {
	if services == nil || services.ExecutionStore == nil || services.RunEvidence == nil || services.ReviewCandidates == nil || services.Workspaces == nil || !store.SupportsReviewStore(services.ExecutionStore) {
		return nil
	}
	reviews := services.ExecutionStore.(store.ReviewStore)
	service, err := NewReviewService(reviews, services.ExecutionStore, services.RunEvidence, services.Workspaces, services.ReviewCandidates)
	if err != nil {
		return nil
	}
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
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return store.CompleteReviewApprovalResult{}, translateStoreError(err, "project")
	}
	privateCandidate, err := s.candidates.Open(ctx, begin.Run.ID)
	if err != nil {
		s.persistApprovalFailure(ctx, projectID, reviewID, "Review candidate delivery snapshot is unavailable")
		return store.CompleteReviewApprovalResult{}, NewError("review_evidence_invalid", "Review candidate delivery snapshot is unavailable", err)
	}
	candidate := acceptedCandidateFromPrivate(privateCandidate)
	revision, err := s.applier.ApplyReviewedCandidate(ctx, project, begin.Review.ID, candidate)
	if err != nil {
		s.persistApprovalFailure(ctx, projectID, reviewID, "candidate could not be applied to the current Project Workspace")
		return store.CompleteReviewApprovalResult{}, err
	}
	result, err := s.store.CompleteReviewApproval(ctx, store.CompleteReviewApprovalCommand{
		ProjectID:        projectID,
		ReviewID:         reviewID,
		AcceptedRevision: revision,
	})
	if err != nil {
		// Do not convert a post-apply persistence failure into a failed approval.
		// The approval intent plus acceptance commit is intentionally recoverable
		// by retrying this command after a crash/database outage.
		return store.CompleteReviewApprovalResult{}, translateStoreError(err, "review")
	}
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
	return result, nil
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

func acceptedCandidateFromPrivate(value evidencepkg.ReviewCandidateSnapshot) workspacepkg.AcceptedCandidate {
	candidate := workspacepkg.AcceptedCandidate{}
	if value.StagedPatch != nil {
		candidate.StagedPatch = workspacepkg.CandidateBlobSource(value.StagedPatch)
	}
	if value.UnstagedPatch != nil {
		candidate.UnstagedPatch = workspacepkg.CandidateBlobSource(value.UnstagedPatch)
	}
	candidate.Files = make([]workspacepkg.CandidateFileSource, 0, len(value.Files))
	for _, file := range value.Files {
		candidate.Files = append(candidate.Files, workspacepkg.CandidateFileSource{
			Path:       file.Path,
			Executable: file.Executable,
			Chunks:     []workspacepkg.CandidateBlobSource{workspacepkg.CandidateBlobSource(file.Source)},
		})
	}
	return candidate
}
