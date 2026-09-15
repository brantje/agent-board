package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (f *fakeControlPlaneStore) PlaceIssue(_ context.Context, input store.IssueBoardPlacement) (store.IssueMutationResult, error) {
	if input.ProjectID != projectID || input.IssueID != issueID {
		return store.IssueMutationResult{}, store.ErrNotFound
	}
	position := int64(2)
	if input.BeforeIssueID != nil {
		if *input.BeforeIssueID != otherID {
			return store.IssueMutationResult{}, store.ErrInvalidArgument
		}
		position = 1
	}
	issue := issueFixture(input.Status)
	issue.BoardPosition = position
	return store.IssueMutationResult{Issue: issue}, nil
}

func TestPlaceIssueHTTP(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPatch,
		"/api/projects/"+projectID+"/issues/"+issueKey+"/board",
		strings.NewReader(`{"status":"IN_PROGRESS","beforeIssueId":"`+otherIssueKey+`"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response IssueDTO
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != issueKey || response.Status != "IN_PROGRESS" || response.BoardPosition != 1 {
		t.Fatalf("response=%+v", response)
	}
}

func TestPlaceIssueHTTPAllowsExplicitNullAnchor(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPatch,
		"/api/projects/"+projectID+"/issues/"+issueKey+"/board",
		strings.NewReader(`{"status":"TODO","beforeIssueId":null}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response IssueDTO
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "TODO" || response.BoardPosition != 2 {
		t.Fatalf("response=%+v", response)
	}
}

func TestPlaceIssueHTTPRejectsInvalidStatus(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPatch,
		"/api/projects/"+projectID+"/issues/"+issueKey+"/board",
		strings.NewReader(`{"status":"QUEUED"}`),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "invalid_argument") {
		t.Fatalf("body=%s", recorder.Body.String())
	}
}
