package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestQuestionRouteGetsSingleQuestionAndValidatesFilters(t *testing.T) {
	questionStore := &apiQuestionStore{questions: []store.Question{{
		ID:        otherID,
		ProjectID: projectID,
		IssueID:   issueID,
		RunID:     runID,
		Prompt:    "Which strategy?",
		Kind:      "TEXT",
		Blocking:  true,
		Status:    "OPEN",
	}}}
	questionService, err := app.NewQuestionService(questionStore)
	if err != nil {
		t.Fatal(err)
	}
	router := newRouter(app.New(&fakeControlPlaneStore{}), nil, nil, nil, questionService)

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/questions/"+otherID, nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "Which strategy?") || !strings.Contains(get.Body.String(), `"options":[]`) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}

	invalidID := httptest.NewRecorder()
	router.ServeHTTP(invalidID, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/questions/not-a-uuid", nil))
	if invalidID.Code != http.StatusBadRequest {
		t.Fatalf("invalid question id status=%d body=%s", invalidID.Code, invalidID.Body.String())
	}

	invalidIssue := httptest.NewRecorder()
	router.ServeHTTP(invalidIssue, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/questions?issueId=bad", nil))
	if invalidIssue.Code != http.StatusBadRequest {
		t.Fatalf("invalid issue filter status=%d body=%s", invalidIssue.Code, invalidIssue.Body.String())
	}

	invalidRun := httptest.NewRecorder()
	router.ServeHTTP(invalidRun, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/questions?runId=bad", nil))
	if invalidRun.Code != http.StatusBadRequest {
		t.Fatalf("invalid run filter status=%d body=%s", invalidRun.Code, invalidRun.Body.String())
	}

	invalidStatus := httptest.NewRecorder()
	router.ServeHTTP(invalidStatus, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/projects/"+projectID+"/questions?status=unknown", nil))
	if invalidStatus.Code != http.StatusBadRequest {
		t.Fatalf("invalid status filter status=%d body=%s", invalidStatus.Code, invalidStatus.Body.String())
	}
}
