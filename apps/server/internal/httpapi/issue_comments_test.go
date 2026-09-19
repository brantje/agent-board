package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	httpCommentID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	httpReplyID   = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

type issueCommentHTTPStore struct {
	*fakeControlPlaneStore
	comments []store.IssueComment
	events   []store.Event
}

func (s *issueCommentHTTPStore) ListIssueComments(_ context.Context, pid, id string) ([]store.IssueComment, error) {
	if pid != projectID || id != issueID {
		return nil, store.ErrNotFound
	}
	return append([]store.IssueComment(nil), s.comments...), nil
}

func (s *issueCommentHTTPStore) CreateIssueComment(_ context.Context, pid string, input store.IssueComment) (store.IssueCommentMutationResult, error) {
	if pid != projectID || input.IssueID != issueID {
		return store.IssueCommentMutationResult{}, store.ErrNotFound
	}
	input.ID = httpCommentID
	if input.ParentCommentID != nil {
		input.ID = httpReplyID
	}
	input.AuthorName = "Member"
	input.CreatedAt = time.Date(2026, 9, 19, 1, 0, len(s.comments), 0, time.UTC)
	input.UpdatedAt = input.CreatedAt
	s.comments = append(s.comments, input)
	issueID := input.IssueID
	event := store.Event{
		ID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", Type: "issue.comment_created",
		ProjectID: pid, IssueID: &issueID, OccurredAt: input.CreatedAt, Actor: store.EmptyObject, Payload: store.EmptyObject,
	}
	return store.IssueCommentMutationResult{Comment: input, Events: []store.Event{event}}, nil
}

func (s *issueCommentHTTPStore) ListIssueTimelineEvents(_ context.Context, pid, id string) ([]store.Event, error) {
	if pid != projectID || id != issueID {
		return nil, store.ErrNotFound
	}
	return append([]store.Event(nil), s.events...), nil
}

func newIssueCommentHTTPFixture(t *testing.T) (*projectAccessHTTPFixture, *issueCommentHTTPStore) {
	t.Helper()
	fixture := newProjectAccessHTTPFixture(t)
	database := &issueCommentHTTPStore{fakeControlPlaneStore: &fakeControlPlaneStore{}}
	control := app.New(database)
	access, err := app.NewProjectAccessService(control, fixture.access)
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler = NewRouterWithApplication(&app.Services{ControlPlane: control, Auth: fixture.auth, ProjectAccess: access})
	return fixture, database
}

func TestIssueCommentHTTPCreateReplyReadAndTimeline(t *testing.T) {
	fixture, database := newIssueCommentHTTPFixture(t)
	member, memberToken := fixture.createUser(t, "comment-member", store.DeploymentRoleMember)
	viewer, viewerToken := fixture.createUser(t, "comment-viewer", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, member.ID)] = store.ProjectRoleMember
	fixture.access.roles[projectGrantKey(projectID, viewer.ID)] = store.ProjectRoleViewer

	base := "/api/projects/" + projectID + "/issues/" + issueKey + "/comments"
	denied := authHTTPRequest(t, fixture.handler, http.MethodPost, base, `{"body":"viewer cannot post"}`, bearer(viewerToken))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d body=%s", denied.Code, denied.Body.String())
	}

	created := authHTTPRequest(t, fixture.handler, http.MethodPost, base, `{"body":"Hello **world** @nobody"}`, bearer(memberToken))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var root IssueCommentDTO
	if err := json.Unmarshal(created.Body.Bytes(), &root); err != nil {
		t.Fatal(err)
	}
	if root.ID != httpCommentID || root.IssueID != issueKey || root.ParentCommentID != nil || root.Author.Type != store.ActorTypeHuman || root.Author.ID != member.ID {
		t.Fatalf("root=%+v", root)
	}

	reply := authHTTPRequest(t, fixture.handler, http.MethodPost, base, `{"parentCommentId":"`+httpCommentID+`","body":"Reply"}`, bearer(memberToken))
	if reply.Code != http.StatusCreated {
		t.Fatalf("reply status=%d body=%s", reply.Code, reply.Body.String())
	}
	var child IssueCommentDTO
	if err := json.Unmarshal(reply.Body.Bytes(), &child); err != nil {
		t.Fatal(err)
	}
	if child.ParentCommentID == nil || *child.ParentCommentID != httpCommentID {
		t.Fatalf("reply=%+v", child)
	}

	badParent := authHTTPRequest(t, fixture.handler, http.MethodPost, base, `{"parentCommentId":"not-a-uuid","body":"bad"}`, bearer(memberToken))
	if badParent.Code != http.StatusBadRequest {
		t.Fatalf("bad parent status=%d body=%s", badParent.Code, badParent.Body.String())
	}

	list := authHTTPRequest(t, fixture.handler, http.MethodGet, base, "", bearer(viewerToken))
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var comments []IssueCommentDTO
	if err := json.Unmarshal(list.Body.Bytes(), &comments); err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 {
		t.Fatalf("comments=%+v", comments)
	}

	at := time.Date(2026, 9, 19, 0, 59, 0, 0, time.UTC)
	issueIDValue := issueID
	database.events = []store.Event{
		{ID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", Type: "issue.updated", ProjectID: projectID, IssueID: &issueIDValue, OccurredAt: at, Actor: store.EmptyObject, Payload: store.EmptyObject},
	}
	timeline := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID+"/issues/"+issueKey+"/timeline", "", bearer(viewerToken))
	if timeline.Code != http.StatusOK {
		t.Fatalf("timeline status=%d body=%s", timeline.Code, timeline.Body.String())
	}
	var entries []IssueTimelineEntryDTO
	if err := json.Unmarshal(timeline.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Kind != store.IssueTimelineKindActivity {
		t.Fatalf("timeline=%+v", entries)
	}

	_, outsideToken := fixture.createUser(t, "comment-outside", store.DeploymentRoleMember)
	hidden := authHTTPRequest(t, fixture.handler, http.MethodGet, base, "", bearer(outsideToken))
	if hidden.Code != http.StatusNotFound {
		t.Fatalf("outside status=%d body=%s", hidden.Code, hidden.Body.String())
	}
}


func TestIssueCommentLowLevelRouterReadCompatibilityAndWriteFailClosed(t *testing.T) {
	database := &issueCommentHTTPStore{fakeControlPlaneStore: &fakeControlPlaneStore{}}
	control := app.New(database)
	router := NewRouter(control)
	base := "/api/projects/" + projectID + "/issues/" + issueKey

	badCommentsProject := httptest.NewRecorder()
	router.ServeHTTP(badCommentsProject, httptest.NewRequest(http.MethodGet, "/api/projects/not-a-uuid/issues/"+issueKey+"/comments", nil))
	if badCommentsProject.Code != http.StatusBadRequest {
		t.Fatalf("bad comments project status=%d body=%s", badCommentsProject.Code, badCommentsProject.Body.String())
	}
	badTimelineProject := httptest.NewRecorder()
	router.ServeHTTP(badTimelineProject, httptest.NewRequest(http.MethodGet, "/api/projects/not-a-uuid/issues/"+issueKey+"/timeline", nil))
	if badTimelineProject.Code != http.StatusBadRequest {
		t.Fatalf("bad timeline project status=%d body=%s", badTimelineProject.Code, badTimelineProject.Body.String())
	}

	comments := httptest.NewRecorder()
	router.ServeHTTP(comments, httptest.NewRequest(http.MethodGet, base+"/comments", nil))
	if comments.Code != http.StatusOK {
		t.Fatalf("low-level comments status=%d body=%s", comments.Code, comments.Body.String())
	}

	timeline := httptest.NewRecorder()
	router.ServeHTTP(timeline, httptest.NewRequest(http.MethodGet, base+"/timeline", nil))
	if timeline.Code != http.StatusOK {
		t.Fatalf("low-level timeline status=%d body=%s", timeline.Code, timeline.Body.String())
	}

	invalidParent := httptest.NewRecorder()
	router.ServeHTTP(invalidParent, httptest.NewRequest(http.MethodPost, base+"/comments", strings.NewReader(`{"parentCommentId":"not-a-uuid","body":"reply"}`)))
	if invalidParent.Code != http.StatusBadRequest {
		t.Fatalf("invalid parent status=%d body=%s", invalidParent.Code, invalidParent.Body.String())
	}

	create := httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, base+"/comments", strings.NewReader(`{"body":"cannot forge unauthenticated write"}`)))
	if create.Code != http.StatusUnauthorized {
		t.Fatalf("low-level create status=%d body=%s", create.Code, create.Body.String())
	}
}
