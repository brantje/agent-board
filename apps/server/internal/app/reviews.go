package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

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
	store    store.ReviewStore
	projects reviewProjectStore
	evidence *RunEvidenceService
	applier  reviewCandidateApplier
}

func NewReviewService(reviewStore store.ReviewStore, projects reviewProjectStore, evidence *RunEvidenceService, applier reviewCandidateApplier) (*ReviewService, error) {
	if reviewStore == nil || projects == nil || evidence == nil || applier == nil {
		return nil, fmt.Errorf("review service dependencies are required")
	}
	return &ReviewService{store: reviewStore, projects: projects, evidence: evidence, applier: applier}, nil
}

// ReviewServiceFromServices exposes Review only when the configured persistence
// and execution-evidence stack has the required optional capability.
func ReviewServiceFromServices(services *Services) *ReviewService {
	if services == nil || services.ExecutionStore == nil || services.RunEvidence == nil || services.Workspaces == nil || !store.SupportsReviewStore(services.ExecutionStore) {
		return nil
	}
	reviews := services.ExecutionStore.(store.ReviewStore)
	service, err := NewReviewService(reviews, services.ExecutionStore, services.RunEvidence, services.Workspaces)
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
	evidence, err := s.evidence.Inspect(ctx, projectID, begin.Run.ID)
	if err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	candidate, err := s.acceptedCandidate(evidence)
	if err != nil {
		s.persistApprovalFailure(ctx, projectID, reviewID, "Review candidate evidence is incomplete")
		return store.CompleteReviewApprovalResult{}, NewError("review_evidence_invalid", "Review candidate evidence is incomplete", err)
	}
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

type candidateChunk struct {
	index  int
	source workspacepkg.CandidateBlobSource
}

type candidateFileSet struct {
	count      int
	executable bool
	chunks     []candidateChunk
}

func (s *ReviewService) acceptedCandidate(value RunEvidence) (workspacepkg.AcceptedCandidate, error) {
	candidate := workspacepkg.AcceptedCandidate{}
	manifestCount := 0
	files := make(map[string]*candidateFileSet)
	for _, artifact := range value.Artifacts {
		source := s.artifactSource(value.Run.ProjectID, value.Run.ID, artifact.ID)
		switch {
		case artifact.Kind == "candidate_manifest":
			manifestCount++
		case artifact.Kind == "candidate_patch" && artifact.Name == "candidate-staged.patch":
			if candidate.StagedPatch != nil {
				return workspacepkg.AcceptedCandidate{}, fmt.Errorf("duplicate staged candidate patch")
			}
			candidate.StagedPatch = source
		case artifact.Kind == "candidate_patch" && artifact.Name == "candidate-unstaged.patch":
			if candidate.UnstagedPatch != nil {
				return workspacepkg.AcceptedCandidate{}, fmt.Errorf("duplicate unstaged candidate patch")
			}
			candidate.UnstagedPatch = source
		case artifact.Kind == "candidate_file", artifact.Kind == "candidate_file_chunk":
			var metadata struct {
				Path       string `json:"path"`
				Executable bool   `json:"executable"`
				ChunkIndex int    `json:"chunkIndex"`
				ChunkCount int    `json:"chunkCount"`
			}
			if err := json.Unmarshal(artifact.SafeMetadata, &metadata); err != nil {
				return workspacepkg.AcceptedCandidate{}, fmt.Errorf("decode candidate file metadata: %w", err)
			}
			if strings.TrimSpace(metadata.Path) == "" {
				return workspacepkg.AcceptedCandidate{}, fmt.Errorf("candidate file path is missing")
			}
			if artifact.Kind == "candidate_file" {
				metadata.ChunkIndex = 0
				metadata.ChunkCount = 1
			}
			if metadata.ChunkIndex < 0 || metadata.ChunkCount < 1 || metadata.ChunkIndex >= metadata.ChunkCount {
				return workspacepkg.AcceptedCandidate{}, fmt.Errorf("candidate file chunk metadata is invalid")
			}
			set := files[metadata.Path]
			if set == nil {
				set = &candidateFileSet{count: metadata.ChunkCount, executable: metadata.Executable}
				files[metadata.Path] = set
			}
			if set.count != metadata.ChunkCount {
				return workspacepkg.AcceptedCandidate{}, fmt.Errorf("candidate file chunk count changed")
			}
			if set.executable != metadata.Executable {
				return workspacepkg.AcceptedCandidate{}, fmt.Errorf("candidate file executable metadata changed")
			}
			set.chunks = append(set.chunks, candidateChunk{index: metadata.ChunkIndex, source: source})
		}
	}
	if manifestCount != 1 {
		return workspacepkg.AcceptedCandidate{}, fmt.Errorf("candidate manifest count=%d, want 1", manifestCount)
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		set := files[path]
		if len(set.chunks) != set.count {
			return workspacepkg.AcceptedCandidate{}, fmt.Errorf("candidate file %q has %d/%d chunks", path, len(set.chunks), set.count)
		}
		sort.Slice(set.chunks, func(i, j int) bool { return set.chunks[i].index < set.chunks[j].index })
		chunks := make([]workspacepkg.CandidateBlobSource, 0, len(set.chunks))
		for index, chunk := range set.chunks {
			if chunk.index != index {
				return workspacepkg.AcceptedCandidate{}, fmt.Errorf("candidate file %q has duplicate or missing chunk indexes", path)
			}
			chunks = append(chunks, chunk.source)
		}
		candidate.Files = append(candidate.Files, workspacepkg.CandidateFileSource{Path: path, Chunks: chunks, Executable: set.executable})
	}
	return candidate, nil
}

func (s *ReviewService) artifactSource(projectID, runID, artifactID string) workspacepkg.CandidateBlobSource {
	return func(ctx context.Context) (io.ReadCloser, error) {
		_, reader, err := s.evidence.OpenArtifact(ctx, projectID, runID, artifactID)
		if err != nil {
			return nil, err
		}
		return reader, nil
	}
}
