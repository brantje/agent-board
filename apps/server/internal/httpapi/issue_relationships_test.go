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

type issueRelationshipHTTPStore struct {
	*fakeControlPlaneStore
	relationships []store.IssueRelationship
	duplicate     bool
}

func (s *issueRelationshipHTTPStore) GetIssue(_ context.Context, pid, id string) (store.Issue, error) {
	if pid != projectID {
		return store.Issue{}, store.ErrNotFound
	}
	switch id {
	case issueID:
		return store.Issue{ID: issueID, ProjectID: projectID, Title: "Source", Status: "TODO", Priority: 2}, nil
	case otherID:
		return store.Issue{ID: otherID, ProjectID: projectID, Title: "Target", Status: "BACKLOG", Priority: 1}, nil
	default:
		return store.Issue{}, store.ErrNotFound
	}
}

func (s *issueRelationshipHTTPStore) ListIssueRelationships(context.Context, string, string) ([]store.IssueRelationship, error) {
	return s.relationships, nil
}

func (s *issueRelationshipHTTPStore) CreateIssueRelationship(_ context.Context, value store.IssueRelationship) (store.IssueRelationship, error) {
	if s.duplicate {
		return store.IssueRelationship{}, store.ErrConflict
	}
	value.ID = modelID
	s.relationships = append(s.relationships, value)
	return value, nil
}

func (s *issueRelationshipHTTPStore) DeleteIssueRelationship(_ context.Context, _, _, id string) error {
	if id != modelID || len(s.relationships) == 0 {
		return store.ErrNotFound
	}
	s.relationships = nil
	return nil
}

func decodeErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Error.Code
}

func assertAPIError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status || decodeErrorCode(t, recorder) != code {
		t.Fatalf("status=%d body=%s want status=%d code=%s", recorder.Code, recorder.Body.String(), status, code)
	}
}

func TestIssuePriorityAndRelationshipHTTPContract(t *testing.T) {
	backend := &issueRelationshipHTTPStore{fakeControlPlaneStore: &fakeControlPlaneStore{}}
	router := NewRouter(app.New(backend))

	create := httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues", strings.NewReader(`{"title":"Prioritized","priority":4}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created IssueDTO
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Priority != 4 {
		t.Fatalf("priority=%d want 4", created.Priority)
	}

	invalidPriority := httptest.NewRecorder()
	router.ServeHTTP(invalidPriority, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues", strings.NewReader(`{"title":"Too important","priority":5}`)))
	assertAPIError(t, invalidPriority, http.StatusBadRequest, "invalid_argument")

	createRelationship := httptest.NewRecorder()
	router.ServeHTTP(createRelationship, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships", strings.NewReader(`{"targetIssueId":"`+otherID+`","type":"blocks"}`)))
	if createRelationship.Code != http.StatusCreated {
		t.Fatalf("relationship create status=%d body=%s", createRelationship.Code, createRelationship.Body.String())
	}
	var relationship IssueRelationshipDTO
	if err := json.Unmarshal(createRelationship.Body.Bytes(), &relationship); err != nil {
		t.Fatal(err)
	}
	if relationship.SourceIssueID != issueID || relationship.TargetIssueID != otherID || relationship.Type != "blocks" {
		t.Fatalf("relationship=%+v", relationship)
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"type":"blocks"`) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	backend.duplicate = true
	duplicate := httptest.NewRecorder()
	router.ServeHTTP(duplicate, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships", strings.NewReader(`{"targetIssueId":"`+otherID+`","type":"blocks"}`)))
	assertAPIError(t, duplicate, http.StatusConflict, "issue_relationship_exists")
	backend.duplicate = false

	self := httptest.NewRecorder()
	router.ServeHTTP(self, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships", strings.NewReader(`{"targetIssueId":"`+issueID+`","type":"related_to"}`)))
	assertAPIError(t, self, http.StatusBadRequest, "issue_relationship_self_reference")

	invalidTargetID := httptest.NewRecorder()
	router.ServeHTTP(invalidTargetID, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships", strings.NewReader(`{"targetIssueId":"not-a-uuid","type":"blocks"}`)))
	assertAPIError(t, invalidTargetID, http.StatusBadRequest, "invalid_id")

	missingTarget := httptest.NewRecorder()
	router.ServeHTTP(missingTarget, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships", strings.NewReader(`{"targetIssueId":"`+modelID+`","type":"blocks"}`)))
	assertAPIError(t, missingTarget, http.StatusNotFound, "issue_relationship_target_not_found")

	invalidType := httptest.NewRecorder()
	router.ServeHTTP(invalidType, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships", strings.NewReader(`{"targetIssueId":"`+otherID+`","type":"parents"}`)))
	assertAPIError(t, invalidType, http.StatusBadRequest, "invalid_argument")

	invalidBody := httptest.NewRecorder()
	router.ServeHTTP(invalidBody, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships", strings.NewReader(`{"targetIssueId":`)))
	assertAPIError(t, invalidBody, http.StatusBadRequest, "invalid_request")

	invalidSourceID := httptest.NewRecorder()
	router.ServeHTTP(invalidSourceID, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/issues/not-a-uuid/relationships", nil))
	assertAPIError(t, invalidSourceID, http.StatusBadRequest, "invalid_id")

	missingSource := httptest.NewRecorder()
	router.ServeHTTP(missingSource, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/issues/"+modelID+"/relationships", nil))
	assertAPIError(t, missingSource, http.StatusNotFound, "issue_not_found")

	remove := httptest.NewRecorder()
	router.ServeHTTP(remove, httptest.NewRequest(http.MethodDelete, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships/"+modelID, nil))
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", remove.Code, remove.Body.String())
	}

	missingRelationship := httptest.NewRecorder()
	router.ServeHTTP(missingRelationship, httptest.NewRequest(http.MethodDelete, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships/"+modelID, nil))
	assertAPIError(t, missingRelationship, http.StatusNotFound, "issue_relationship_not_found")

	invalidRelationshipID := httptest.NewRecorder()
	router.ServeHTTP(invalidRelationshipID, httptest.NewRequest(http.MethodDelete, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships/not-a-uuid", nil))
	assertAPIError(t, invalidRelationshipID, http.StatusBadRequest, "invalid_id")
}
