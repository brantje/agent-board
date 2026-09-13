package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (f *fakeControlPlaneStore) SetIssueAssignee(_ context.Context, pid, id string, target *store.Assignee, _ json.RawMessage) (store.Issue, store.Event, error) {
	if pid != projectID || id != issueID {
		return store.Issue{}, store.Event{}, store.ErrNotFound
	}
	issue := issueFixture("TODO")
	if target != nil {
		issue.AssigneeType = &target.Type
		issue.AssigneeID = &target.ID
		issue.AssigneeName = stringPtr("Owner")
	}
	return issue, store.Event{}, nil
}
func (f *fakeControlPlaneStore) ListIssueAssignees(_ context.Context, pid string) ([]store.Assignee, error) {
	if pid != projectID {
		return nil, store.ErrNotFound
	}
	return []store.Assignee{{Type: "USER", ID: otherID, Name: "Owner"}, {Type: "AGENT", ID: agentID, Name: "Agent"}}, nil
}

func TestGenericAssignmentHTTPContract(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"assignedTo":{"type":"USER","id":"` + otherID + `"}}`, 200},
		{`{"assignedTo":{"type":"AGENT","id":"` + agentID + `"}}`, 200},
		{`{"assignedTo":null}`, 200},
		{`{}`, 400}, {`{"agentId":"` + agentID + `"}`, 400},
		{`{"assignedTo":{"type":"GROUP","id":"` + otherID + `"}}`, 400},
		{`{"assignedTo":{"type":"USER","id":"bad"}}`, 400},
		{`{"assignedTo":{"type":"USER","id":"` + otherID + `","name":"forged"}}`, 400},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/projects/"+projectID+"/issues/"+issueKey+"/assignment", strings.NewReader(tc.body))
		router.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("%s status=%d body=%s", tc.body, rec.Code, rec.Body.String())
		}
		if tc.code == 200 {
			if strings.Contains(rec.Body.String(), "assignedAgentId") || !strings.Contains(rec.Body.String(), "assignedTo") || strings.Contains(rec.Body.String(), `"run":`) {
				t.Fatalf("bad contract: %s", rec.Body.String())
			}
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/projects/"+projectID+"/assignees", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":"USER"`) || !strings.Contains(rec.Body.String(), `"type":"AGENT"`) {
		t.Fatalf("directory: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGenericAssignmentHTTPProjectAuthorization(t *testing.T) {
	f := newProjectAccessHTTPFixture(t)
	member, memberToken := f.createUser(t, "member", "member")
	viewer, viewerToken := f.createUser(t, "viewer", "member")
	_, outsideToken := f.createUser(t, "outside", "member")
	f.access.roles[projectGrantKey(projectID, member.ID)] = "member"
	f.access.roles[projectGrantKey(projectID, viewer.ID)] = "viewer"
	for _, tc := range []struct {
		token, method, path, body string
		code                      int
	}{
		{memberToken, "POST", "/issues/" + issueKey + "/assignment", `{"assignedTo":null}`, 200},
		{viewerToken, "POST", "/issues/" + issueKey + "/assignment", `{"assignedTo":null}`, 403},
		{outsideToken, "POST", "/issues/" + issueKey + "/assignment", `{"assignedTo":null}`, 404},
		{memberToken, "GET", "/assignees", "", 200},
		{viewerToken, "GET", "/assignees", "", 200},
		{outsideToken, "GET", "/assignees", "", 404},
		{"", "GET", "/assignees", "", 401},
	} {
		req := httptest.NewRequest(tc.method, "/api/projects/"+projectID+tc.path, strings.NewReader(tc.body))
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("%s %s status=%d want=%d body=%s", tc.method, tc.path, rec.Code, tc.code, rec.Body.String())
		}
	}
}
