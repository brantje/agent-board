package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	evidencepkg "github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reviewServiceStore struct {
	project         store.Project
	run             store.Run
	review          store.Review
	decision        store.Decision
	events          []store.Event
	artifacts       []store.Artifact
	list            []store.Review
	begin           store.BeginReviewApprovalResult
	complete        store.CompleteReviewApprovalResult
	request         store.RequestReviewChangesResult
	beginErr        error
	completeErr     error
	requestErr      error
	failCalls       int
	completeCommand store.CompleteReviewApprovalCommand
	requestCommand  store.RequestReviewChangesCommand
}

func (s *reviewServiceStore) GetProject(context.Context, string) (store.Project, error) {
	return s.project, nil
}
func (s *reviewServiceStore) GetReview(context.Context, string, string) (store.Review, error) {
	return s.review, nil
}
func (s *reviewServiceStore) GetReviewByRun(_ context.Context, projectID, runID string) (store.Review, error) {
	if s.review.ProjectID != projectID || s.review.RunID != runID {
		return store.Review{}, store.ErrNotFound
	}
	return s.review, nil
}
func (s *reviewServiceStore) ListReviews(context.Context, string, store.ReviewFilter) ([]store.Review, error) {
	return append([]store.Review(nil), s.list...), nil
}
func (s *reviewServiceStore) GetReviewRevisions(_ context.Context, _, reviewID string) (string, string, error) {
	if s.review.ID == reviewID {
		return s.review.BaseRevision, s.review.ReviewRevision, nil
	}
	for _, review := range s.list {
		if review.ID == reviewID {
			return review.BaseRevision, review.ReviewRevision, nil
		}
	}
	return "", "", store.ErrNotFound
}
func (s *reviewServiceStore) GetDecision(context.Context, string, string) (store.Decision, error) {
	return s.decision, nil
}
func (s *reviewServiceStore) BeginReviewApproval(context.Context, store.BeginReviewApprovalCommand) (store.BeginReviewApprovalResult, error) {
	return s.begin, s.beginErr
}
func (s *reviewServiceStore) CompleteReviewApproval(_ context.Context, command store.CompleteReviewApprovalCommand) (store.CompleteReviewApprovalResult, error) {
	s.completeCommand = command
	return s.complete, s.completeErr
}
func (s *reviewServiceStore) FailReviewApproval(context.Context, store.FailReviewApprovalCommand) (store.Review, error) {
	s.failCalls++
	return s.review, nil
}
func (s *reviewServiceStore) RequestReviewChanges(_ context.Context, command store.RequestReviewChangesCommand) (store.RequestReviewChangesResult, error) {
	s.requestCommand = command
	return s.request, s.requestErr
}
func (s *reviewServiceStore) GetRun(context.Context, string, string) (store.Run, error) {
	return s.run, nil
}
func (s *reviewServiceStore) GetRunProvenance(context.Context, string, string) (json.RawMessage, error) {
	return json.RawMessage(`{"safe":true}`), nil
}
func (s *reviewServiceStore) ListExecutionSessionsByRun(context.Context, string, string, []string) ([]store.ExecutionSession, error) {
	return nil, nil
}
func (s *reviewServiceStore) ListRunEvents(_ context.Context, _, _ string, after int64, limit int) ([]store.Event, error) {
	values := make([]store.Event, 0, len(s.events))
	for index, event := range s.events {
		sequence := int64(index + 1)
		if event.Sequence == nil {
			event.Sequence = &sequence
		}
		if *event.Sequence <= after {
			continue
		}
		values = append(values, event)
		if limit > 0 && len(values) >= limit {
			break
		}
	}
	return values, nil
}
func (s *reviewServiceStore) GetRawOutputChunk(context.Context, string, string, string) (store.RawOutputChunk, error) {
	return store.RawOutputChunk{}, store.ErrNotFound
}
func (s *reviewServiceStore) ListRawOutputChunks(context.Context, string, string) ([]store.RawOutputChunk, error) {
	return nil, nil
}
func (s *reviewServiceStore) GetArtifact(_ context.Context, _, _, artifactID string) (store.Artifact, error) {
	for _, artifact := range s.artifacts {
		if artifact.ID == artifactID {
			return artifact, nil
		}
	}
	return store.Artifact{}, store.ErrNotFound
}
func (s *reviewServiceStore) ListArtifacts(context.Context, string, string) ([]store.Artifact, error) {
	return append([]store.Artifact(nil), s.artifacts...), nil
}

type reviewBlobStore struct{ values map[string][]byte }

func (s *reviewBlobStore) Put(context.Context, string, io.Reader) (evidencepkg.Blob, error) {
	return evidencepkg.Blob{}, errors.New("unused")
}
func (s *reviewBlobStore) Open(_ context.Context, ref string) (io.ReadCloser, error) {
	value, ok := s.values[ref]
	if !ok {
		return nil, errors.New("missing blob")
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

type reviewCandidateApplierFake struct {
	revision string
	review   store.Review
	calls    int
	err      error
}

func (a *reviewCandidateApplierFake) ApplyReviewedRevision(_ context.Context, _ store.Project, review store.Review) (string, error) {
	a.calls++
	a.review = review
	if a.err != nil {
		return "", a.err
	}
	return a.revision, nil
}

func newReviewServiceForTest(t *testing.T, s *reviewServiceStore, blobs *reviewBlobStore, applier *reviewCandidateApplierFake) *ReviewService {
	t.Helper()
	evidence, err := NewRunEvidenceService(s, blobs)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewReviewService(s, s, evidence, applier)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestReviewServiceGetUsesPinnedEvidenceAndExplicitTestStatus(t *testing.T) {
	s := &reviewServiceStore{
		run: store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1"},
		review: store.Review{
			ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: "run-1", Status: "PENDING",
			BaseRevision: "base-revision", ReviewRevision: "review-revision",
		},
	}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	inspection, err := service.Get(context.Background(), "project-1", "review-1")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Evidence.Run.ID != "run-1" || inspection.TestStatus != ReviewTestsNotRun || inspection.Review.ReviewRevision != "review-revision" {
		t.Fatalf("inspection=%+v", inspection)
	}

	s.events = []store.Event{{Type: "test.started"}}
	inspection, err = service.Get(context.Background(), "project-1", "review-1")
	if err != nil || inspection.TestStatus != ReviewTestsUnknown {
		t.Fatalf("test status=%s err=%v", inspection.TestStatus, err)
	}
	s.events = []store.Event{{Type: "test.completed"}}
	inspection, _ = service.Get(context.Background(), "project-1", "review-1")
	if inspection.TestStatus != ReviewTestsPassed {
		t.Fatalf("test status=%s", inspection.TestStatus)
	}
	s.events = []store.Event{{Type: "test.completed"}, {Type: "test.failed"}}
	inspection, _ = service.Get(context.Background(), "project-1", "review-1")
	if inspection.TestStatus != ReviewTestsFailed {
		t.Fatalf("test status=%s", inspection.TestStatus)
	}
}

func TestReviewServiceApproveIntegratesPinnedRevisionForLocalProject(t *testing.T) {
	run := store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1"}
	review := store.Review{
		ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: run.ID, Status: "PENDING",
		BaseRevision: "base-revision", ReviewRevision: "review-revision",
	}
	s := &reviewServiceStore{
		project: store.Project{ID: "project-1", SourceType: store.ProjectSourceLocal, RepositoryPath: "/repo", DefaultBranch: "main"},
		run:     run,
		review:  review,
		begin:   store.BeginReviewApprovalResult{Review: review, Run: run},
		complete: store.CompleteReviewApprovalResult{
			Review:   store.Review{ID: review.ID, Status: "APPROVED"},
			Decision: store.Decision{ID: "decision", Kind: "REVIEW", Outcome: "APPROVED"},
			Run:      store.Run{ID: run.ID, Status: "COMPLETED"},
			Issue:    store.Issue{ID: "issue-1", Status: "DONE"},
		},
	}
	applier := &reviewCandidateApplierFake{revision: "accepted-revision"}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, applier)
	result, err := service.Approve(context.Background(), "project-1", "review-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Issue.Status != "DONE" || applier.calls != 1 || applier.review.ReviewRevision != "review-revision" {
		t.Fatalf("result=%+v applier=%+v", result, applier)
	}
	if s.completeCommand.AcceptedRevision != "accepted-revision" || !s.completeCommand.DeliveryComplete {
		t.Fatalf("complete command=%+v", s.completeCommand)
	}
}

func TestReviewServiceApproveRemotePinsRevisionWithoutDelivery(t *testing.T) {
	run := store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1", Status: "READY_FOR_REVIEW"}
	review := store.Review{
		ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: run.ID, Status: "PENDING",
		BaseRevision: "base-revision", ReviewRevision: "review-revision",
	}
	s := &reviewServiceStore{
		project: store.Project{ID: "project-1", SourceType: store.ProjectSourceGit},
		run: run, review: review, begin: store.BeginReviewApprovalResult{Review: review, Run: run},
		complete: store.CompleteReviewApprovalResult{
			Review: store.Review{ID: review.ID, Status: "APPROVED"},
			Run: run, Issue: store.Issue{ID: "issue-1", Status: "REVIEW"},
		},
	}
	applier := &reviewCandidateApplierFake{revision: "should-not-be-used"}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, applier)
	result, err := service.Approve(context.Background(), "project-1", "review-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if applier.calls != 0 || s.completeCommand.AcceptedRevision != "review-revision" || s.completeCommand.DeliveryComplete {
		t.Fatalf("applier=%+v command=%+v", applier, s.completeCommand)
	}
	if result.Run.Status != "READY_FOR_REVIEW" || result.Issue.Status != "REVIEW" {
		t.Fatalf("remote approval pretended delivery completed: %+v", result)
	}
}

func TestReviewServiceApproveMarksMissingGitIdentityRetryable(t *testing.T) {
	run := store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1"}
	review := store.Review{ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: run.ID, Status: "PENDING"}
	s := &reviewServiceStore{project: store.Project{ID: "project-1"}, run: run, review: review, begin: store.BeginReviewApprovalResult{Review: review, Run: run}}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	_, err := service.Approve(context.Background(), "project-1", "review-1", nil)
	appErr, ok := AsError(err)
	if !ok || appErr.Code != "review_evidence_invalid" {
		t.Fatalf("err=%v", err)
	}
	if s.failCalls != 1 {
		t.Fatalf("fail calls=%d", s.failCalls)
	}
}

func TestReviewServiceRequestChangesForwardsFeedback(t *testing.T) {
	s := &reviewServiceStore{request: store.RequestReviewChangesResult{Review: store.Review{ID: "review-1", Status: "CHANGES_REQUESTED"}}}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	result, err := service.RequestChanges(context.Background(), "project-1", "review-1", " fix it ", nil)
	if err != nil || result.Review.Status != "CHANGES_REQUESTED" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if s.requestCommand.Feedback != " fix it " {
		t.Fatalf("command=%+v", s.requestCommand)
	}
}

func TestReviewServicePublishesPersistedEventsAfterCommit(t *testing.T) {
	runID := "run-1"
	s := &reviewServiceStore{request: store.RequestReviewChangesResult{
		Review: store.Review{ID: "review-1", Status: "CHANGES_REQUESTED"},
		Events: []store.Event{{ID: "event-review", Type: "review.changes_requested", RunID: &runID}},
	}}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	publisher := &capturingPublisher{}
	service.publisher = publisher
	result, err := service.RequestChanges(context.Background(), "project-1", "review-1", "fix", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].ID != "event-review" {
		t.Fatalf("result events=%+v", result.Events)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != "review.changes_requested" {
		t.Fatalf("published=%+v", publisher.events)
	}
}
