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
	if duplicate.Code != http.StatusConflict || decodeErrorCode(t, duplicate) != "issue_relationship_exists" {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	backend.duplicate = false

	self := httptest.NewRecorder()
	router.ServeHTTP(self, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships", strings.NewReader(`{"targetIssueId":"`+issueID+`","type":"related_to"}`)))
	if self.Code != http.StatusBadRequest || decodeErrorCode(t, self) != "issue_relationship_self_reference" {
		t.Fatalf("self status=%d body=%s", self.Code, self.Body.String())
	}

	remove := httptest.NewRecorder()
	router.ServeHTTP(remove, httptest.NewRequest(http.MethodDelete, "/api/projects/"+projectID+"/issues/"+issueID+"/relationships/"+modelID, nil))
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", remove.Code, remove.Body.String())
	}
}
