package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type startRunHTTPStore struct {
	*fakeControlPlaneStore
	err   error
	calls int
}

func (s *startRunHTTPStore) StartIssueRun(context.Context, string, string) (store.Run, store.Event, error) {
	s.calls++
	return store.Run{ID: otherID, IssueID: issueID, ProjectID: projectID, AgentID: stringPtr(agentID), Status: "QUEUED"}, store.Event{}, s.err
}
func (s *startRunHTTPStore) ReconcileIssueExecution(context.Context, store.IssueExecutionFilter) ([]store.Event, error) {
	return nil, nil
}
func TestStartRunHTTPContract(t *testing.T) {
	f := &startRunHTTPStore{fakeControlPlaneStore: &fakeControlPlaneStore{}}
	router := NewRouter(app.New(f))
	for _, tc := range []struct {
		body string
		err  error
		code int
	}{{"", nil, 200}, {`{}`, nil, 200}, {`{"agentId":"` + agentID + `"}`, nil, 400}, {`{"status":"TODO"}`, nil, 400}, {"", store.ErrConflict, 409}, {"", store.ErrNotFound, 404}} {
		f.err = tc.err
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/api/projects/"+projectID+"/issues/"+issueKey+"/runs", strings.NewReader(tc.body)))
		if w.Code != tc.code {
			t.Fatalf("%q: %d %s", tc.body, w.Code, w.Body.String())
		}
		if tc.code == 200 && (!strings.Contains(w.Body.String(), `"status":"QUEUED"`) || !strings.Contains(w.Body.String(), issueKey)) {
			t.Fatal(w.Body.String())
		}
	}
}
func TestStartRunHTTPAuthorization(t *testing.T) {
	f := newProjectAccessHTTPFixture(t)
	_, outside := f.createUser(t, "outside", "member")
	viewer, token := f.createUser(t, "viewer", "member")
	f.access.roles[projectGrantKey(projectID, viewer.ID)] = "viewer"
	for _, tc := range []struct {
		token string
		code  int
	}{{"", 401}, {outside, 404}, {token, 403}} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/projects/"+projectID+"/issues/"+issueKey+"/runs", nil)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		f.handler.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
