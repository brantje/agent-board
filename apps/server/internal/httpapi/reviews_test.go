package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const reviewID = "12121212-1212-4212-8212-121212121212"
const reviewDecisionID = "13131313-1313-4313-8313-131313131313"
const reviewJobID = "14141414-1414-4414-8414-141414141414"
const reviewNextRunID = "15151515-1515-4515-8515-151515151515"
const reviewBaseRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const reviewHeadRevision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type httpReviewStore struct {
	*httpRunEvidenceStore
	feedback   string
	decisionID *string
}

func (s *httpReviewStore) GetProject(_ context.Context, id string) (store.Project, error) {
	if id != projectID {
		return store.Project{}, store.ErrNotFound
	}
	return store.Project{ID: projectID, Name: "Project", SourceType: store.ProjectSourceLocal, RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, nil
}

func (s *httpReviewStore) GetReview(_ context.Context, pid, id string) (store.Review, error) {
	if pid != projectID || id != reviewID {
		return store.Review{}, store.ErrNotFound
	}
	return store.Review{ID: reviewID, ProjectID: projectID, IssueID: issueID, RunID: runID, Status: "PENDING", DecisionID: s.decisionID, BaseRevision: reviewBaseRevision, ReviewRevision: reviewHeadRevision}, nil
}

func (s *httpReviewStore) GetReviewByRun(_ context.Context, pid, id string) (store.Review, error) {
	if pid != projectID || id != runID {
		return store.Review{}, store.ErrNotFound
	}
	return s.GetReview(context.Background(), pid, reviewID)
}

func (s *httpReviewStore) ListReviews(_ context.Context, pid string, _ store.ReviewFilter) ([]store.Review, error) {
	if pid != projectID {
		return nil, store.ErrNotFound
	}
	review, _ := s.GetReview(context.Background(), projectID, reviewID)
	return []store.Review{review}, nil
}

func (s *httpReviewStore) GetReviewRevisions(_ context.Context, pid, id string) (string, string, error) {
	review, err := s.GetReview(context.Background(), pid, id)
	if err != nil {
		return "", "", err
	}
	return review.BaseRevision, review.ReviewRevision, nil
}

func (s *httpReviewStore) GetDecision(_ context.Context, pid, id string) (store.Decision, error) {
	if pid != projectID || id != reviewDecisionID {
		return store.Decision{}, store.ErrNotFound
	}
	return store.Decision{ID: id, ProjectID: pid, Kind: "REVIEW", Outcome: "APPROVED", ActorType: "HUMAN", SafeDetails: store.EmptyObject}, nil
}

func (s *httpReviewStore) BeginReviewApproval(ctx context.Context, command store.BeginReviewApprovalCommand) (store.BeginReviewApprovalResult, error) {
	review, err := s.GetReview(ctx, command.ProjectID, command.ReviewID)
	if err != nil {
		return store.BeginReviewApprovalResult{}, err
	}
	run, err := s.GetRun(ctx, command.ProjectID, review.RunID)
	if err != nil {
		return store.BeginReviewApprovalResult{}, err
	}
	return store.BeginReviewApprovalResult{Review: review, Run: run}, nil
}

func (s *httpReviewStore) CompleteReviewApproval(_ context.Context, command store.CompleteReviewApprovalCommand) (store.CompleteReviewApprovalResult, error) {
	decision := store.Decision{ID: reviewDecisionID, ProjectID: projectID, Kind: "REVIEW", Outcome: "APPROVED", ActorType: "HUMAN", SafeDetails: json.RawMessage(`{"acceptedRevision":"` + command.AcceptedRevision + `"}`)}
	return store.CompleteReviewApprovalResult{
		Review:   store.Review{ID: reviewID, ProjectID: projectID, IssueID: issueID, RunID: runID, Status: "APPROVED", DecisionID: &decision.ID, BaseRevision: reviewBaseRevision, ReviewRevision: reviewHeadRevision},
		Decision: decision,
		Run:      store.Run{ID: runID, ProjectID: projectID, IssueID: issueID, WorkspaceID: workspaceID, Status: "COMPLETED"},
		Issue:    store.Issue{ID: issueID, ProjectID: projectID, Key: issueKey, Number: 1, Status: "DONE"},
	}, nil
}

func (s *httpReviewStore) FailReviewApproval(context.Context, store.FailReviewApprovalCommand) (store.Review, error) {
	return store.Review{ID: reviewID, ProjectID: projectID, Status: "PENDING"}, nil
}

func (s *httpReviewStore) RequestReviewChanges(_ context.Context, command store.RequestReviewChangesCommand) (store.RequestReviewChangesResult, error) {
	if command.ProjectID != projectID || command.ReviewID != reviewID {
		return store.RequestReviewChangesResult{}, store.ErrNotFound
	}
	feedback := strings.TrimSpace(command.Feedback)
	if feedback == "" {
		return store.RequestReviewChangesResult{}, store.ErrInvalidArgument
	}
	s.feedback = feedback
	decision := store.Decision{ID: reviewDecisionID, ProjectID: projectID, Kind: "REVIEW", Outcome: "CHANGES_REQUESTED", ActorType: "HUMAN", SafeDetails: json.RawMessage(`{"feedback":"add regression coverage"}`)}
	return store.RequestReviewChangesResult{
		Review:   store.Review{ID: reviewID, ProjectID: projectID, IssueID: issueID, RunID: runID, Status: "CHANGES_REQUESTED", DecisionID: &decision.ID, BaseRevision: reviewBaseRevision, ReviewRevision: reviewHeadRevision},
		Decision: decision,
		Run:      store.Run{ID: reviewNextRunID, ProjectID: projectID, IssueID: issueID, WorkspaceID: workspaceID, Attempt: 2, Status: "QUEUED"},
		Issue:    store.Issue{ID: issueID, ProjectID: projectID, Key: issueKey, Number: 1, Status: "IN_PROGRESS"},
		Job:      store.SchedulerJob{ID: reviewJobID, ProjectID: projectID, RunID: reviewNextRunID, Kind: "START", State: "QUEUED"},
	}, nil
}

type httpReviewApplier struct{ revision string }

func (a *httpReviewApplier) ApplyReviewedRevision(context.Context, store.Project, store.Review) (string, error) {
	return a.revision, nil
}

func reviewRouter(t *testing.T) (http.Handler, *httpReviewStore) {
	t.Helper()
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := blobs.Put(t.Context(), runID, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	storeFake := &httpReviewStore{httpRunEvidenceStore: &httpRunEvidenceStore{artifactRef: manifest.Ref, artifactSize: manifest.SizeBytes}}
	runEvidence, err := app.NewRunEvidenceService(storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := app.NewReviewService(storeFake, storeFake, runEvidence, &httpReviewApplier{revision: "accepted-revision"})
	if err != nil {
		t.Fatal(err)
	}
	return newRouterWithReviews(app.New(&fakeControlPlaneStore{}), runEvidence, nil, nil, nil, nil, reviews, nil), storeFake
}

func TestReviewRoutesExposeEvidenceAndCommands(t *testing.T) {
	router, reviewStore := reviewRouter(t)

	t.Run("list", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/reviews?status=PENDING", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), reviewID) || !strings.Contains(rec.Body.String(), reviewHeadRevision) {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("list by issue", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/reviews?issueId="+issueKey, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), reviewID) {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("detail", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/reviews/"+reviewID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{reviewID, runID, reviewBaseRevision, reviewHeadRevision, `"testStatus":"PASSED"`, `"evidence"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("detail missing %s: %s", want, body)
			}
		}
	})

	t.Run("detail with decision", func(t *testing.T) {
		decisionID := reviewDecisionID
		reviewStore.decisionID = &decisionID
		defer func() { reviewStore.decisionID = nil }()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/reviews/"+reviewID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"outcome":"APPROVED"`) {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("approve", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/projects/"+projectID+"/reviews/"+reviewID+"/approve", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"DONE"`) {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("request changes", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/projects/"+projectID+"/reviews/"+reviewID+"/request-changes", strings.NewReader(`{"feedback":"add regression coverage"}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), reviewNextRunID) {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if reviewStore.feedback != "add regression coverage" {
			t.Fatalf("feedback=%q", reviewStore.feedback)
		}
	})
}

func TestReviewRoutesValidateInputsAndMissingResources(t *testing.T) {
	router, _ := reviewRouter(t)
	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "invalid project id", method: http.MethodGet, path: "/api/projects/not-a-uuid/reviews", wantStatus: http.StatusBadRequest},
		{name: "unknown project", method: http.MethodGet, path: "/api/projects/" + otherID + "/reviews", wantStatus: http.StatusNotFound},
		{name: "invalid issue id", method: http.MethodGet, path: "/api/projects/" + projectID + "/reviews?issueId=not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "invalid status", method: http.MethodGet, path: "/api/projects/" + projectID + "/reviews?status=BOGUS", wantStatus: http.StatusBadRequest},
		{name: "invalid review id", method: http.MethodGet, path: "/api/projects/" + projectID + "/reviews/not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "missing review detail", method: http.MethodGet, path: "/api/projects/" + projectID + "/reviews/" + otherID, wantStatus: http.StatusNotFound},
		{name: "missing review approval", method: http.MethodPost, path: "/api/projects/" + projectID + "/reviews/" + otherID + "/approve", wantStatus: http.StatusNotFound},
		{name: "unknown request field", method: http.MethodPost, path: "/api/projects/" + projectID + "/reviews/" + reviewID + "/request-changes", body: `{"feedback":"fix","extra":true}`, wantStatus: http.StatusBadRequest},
		{name: "missing feedback", method: http.MethodPost, path: "/api/projects/" + projectID + "/reviews/" + reviewID + "/request-changes", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "missing review changes", method: http.MethodPost, path: "/api/projects/" + projectID + "/reviews/" + otherID + "/request-changes", body: `{"feedback":"fix"}`, wantStatus: http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestReviewDecisionDTODefaultsSafeDetails(t *testing.T) {
	dto := reviewDecisionDTO(store.Decision{ID: reviewDecisionID, Outcome: "APPROVED", ActorType: "HUMAN"})
	if string(dto.SafeDetails) != `{}` {
		t.Fatalf("safeDetails=%s", dto.SafeDetails)
	}
}
