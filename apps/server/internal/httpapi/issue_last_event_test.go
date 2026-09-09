package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type lastEventIssueStore struct {
	fakeControlPlaneStore
}

func (s *lastEventIssueStore) ListIssues(_ context.Context, pid string) ([]store.Issue, error) {
	if pid != projectID {
		return nil, store.ErrNotFound
	}
	issue := issueFixture("TODO")
	issue.LastEvent = &store.Event{
		ID: otherID, SchemaVersion: 1, Type: "question.created", OccurredAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ProjectID: projectID, IssueID: strPtr(issueID), Actor: store.EmptyObject, Payload: store.EmptyObject,
	}
	return []store.Issue{issue}, nil
}

func TestIssueListIncludesLastEvent(t *testing.T) {
	router := NewRouter(app.New(&lastEventIssueStore{}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/issues", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var issues []IssueDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &issues); err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].LastEvent == nil || issues[0].LastEvent.Type != "question.created" {
		t.Fatalf("issues=%s", rec.Body.String())
	}
	if issues[0].LastEvent.IssueID == nil || *issues[0].LastEvent.IssueID != issueID {
		t.Fatalf("lastEvent issue id=%v", issues[0].LastEvent.IssueID)
	}
}

func TestIssueOpenAPIIncludesLastEvent(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "api")
	data, err := os.ReadFile(filepath.Join(root, "schemas", "control-plane.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	if !strings.Contains(doc, "lastEvent:") || !strings.Contains(doc, "EventEvidence") {
		t.Fatalf("Issue schema missing lastEvent: %s", doc)
	}
}
