package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	evidencepkg "github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
	workspacepkg "github.com/brantje/agent-board/apps/server/internal/workspace"
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
func (s *reviewServiceStore) ListReviews(context.Context, string, store.ReviewFilter) ([]store.Review, error) {
	return append([]store.Review(nil), s.list...), nil
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
func (s *reviewServiceStore) GetRuntimeInstance(context.Context, string, string) (store.RuntimeInstance, error) {
	return store.RuntimeInstance{}, store.ErrNotFound
}
func (s *reviewServiceStore) ListRunEvents(context.Context, string, string, int64, int) ([]store.Event, error) {
	return append([]store.Event(nil), s.events...), nil
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
	staged   string
	files    map[string]string
	err      error
}

func (a *reviewCandidateApplierFake) ApplyReviewedCandidate(ctx context.Context, _ store.Project, _ string, candidate workspacepkg.AcceptedCandidate) (string, error) {
	if a.err != nil {
		return "", a.err
	}
	if candidate.StagedPatch != nil {
		reader, err := candidate.StagedPatch(ctx)
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			return "", err
		}
		a.staged = string(data)
	}
	a.files = map[string]string{}
	for _, file := range candidate.Files {
		var content strings.Builder
		for _, chunk := range file.Chunks {
			reader, err := chunk(ctx)
			if err != nil {
				return "", err
			}
			data, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil {
				return "", err
			}
			content.Write(data)
		}
		a.files[file.Path] = content.String()
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
		run:    store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1"},
		review: store.Review{ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: "run-1", Status: "PENDING"},
	}
	service := newReviewServiceForTest(t, s, &reviewBlobStore{values: map[string][]byte{}}, &reviewCandidateApplierFake{})
	inspection, err := service.Get(context.Background(), "project-1", "review-1")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Evidence.Run.ID != "run-1" || inspection.TestStatus != ReviewTestsNotRun {
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

func TestReviewServiceApproveAppliesStoredCandidateBeforeFinalizing(t *testing.T) {
	manifest := store.Artifact{ID: "manifest", ProjectID: "project-1", RunID: "run-1", Kind: "candidate_manifest", StorageRef: "manifest"}
	staged := store.Artifact{ID: "staged", ProjectID: "project-1", RunID: "run-1", Name: "candidate-staged.patch", Kind: "candidate_patch", StorageRef: "staged"}
	fileMetadata, _ := json.Marshal(map[string]string{"path": "new.txt"})
	file := store.Artifact{ID: "file", ProjectID: "project-1", RunID: "run-1", Kind: "candidate_file", StorageRef: "file", SafeMetadata: fileMetadata}
	run := store.Run{ID: "run-1", ProjectID: "project-1", IssueID: "issue-1"}
	review := store.Review{ID: "review-1", ProjectID: "project-1", IssueID: "issue-1", RunID: run.ID, Status: "PENDING"}
	s := &reviewServiceStore{
		project:   store.Project{ID: "project-1", RepositoryPath: "/repo", DefaultBranch: "main"},
		run:       run,
		review:    review,
		artifacts: []store.Artifact{manifest, staged, file},
		begin:     store.BeginReviewApprovalResult{Review: review, Run: run},
		complete: store.CompleteReviewApprovalResult{
			Review:   store.Review{ID: review.ID, Status: "APPROVED"},
			Decision: store.Decision{ID: "decision", Kind: "REVIEW", Outcome: "APPROVED"},
			Run:      store.Run{ID: run.ID, Status: "COMPLETED"},
			Issue:    store.Issue{ID: "issue-1", Status: "DONE"},
		},
	}
	blobs := &reviewBlobStore{values: map[string][]byte{"manifest": []byte(`{}`), "staged": []byte("patch"), "file": []byte("content")}}
	applier := &reviewCandidateApplierFake{revision: "accepted-revision"}
	service := newReviewServiceForTest(t, s, blobs, applier)
	result, err := service.Approve(context.Background(), "project-1", "review-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Issue.Status != "DONE" || applier.staged != "patch" || applier.files["new.txt"] != "content" {
		t.Fatalf("result=%+v applier=%+v", result, applier)
	}
	if s.completeCommand.AcceptedRevision != "accepted-revision" {
		t.Fatalf("complete command=%+v", s.completeCommand)
	}
}

func TestReviewServiceApproveMarksDeterministicEvidenceFailureRetryable(t *testing.T) {
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
