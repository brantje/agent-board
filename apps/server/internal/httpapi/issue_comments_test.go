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


func (s *issueCommentHTTPStore) GetIssueComment(_ context.Context, pid, id, commentID string) (store.IssueComment, error) {
	if pid != projectID || id != issueID {
		return store.IssueComment{}, store.ErrNotFound
	}
	for _, comment := range s.comments {
		if comment.ID == commentID {
			return comment, nil
		}
	}
	return store.IssueComment{}, store.ErrNotFound
}
func (s *issueCommentHTTPStore) UpdateIssueComment(_ context.Context, pid, id, commentID, actorID, body string) (store.IssueCommentMutationResult, error) {
	for index := range s.comments {
		comment := &s.comments[index]
		if pid == projectID && id == issueID && comment.ID == commentID {
			comment.Body = body
			comment.UpdatedAt = comment.UpdatedAt.Add(time.Second)
			event := store.Event{ID: "abababab-abab-4aba-8aba-abababababab", Type: "issue.comment_changed", ProjectID: pid, IssueID: &id}
			return store.IssueCommentMutationResult{Comment: *comment, Events: []store.Event{event}}, nil
		}
	}
	return store.IssueCommentMutationResult{}, store.ErrNotFound
}
func (s *issueCommentHTTPStore) DeleteIssueComment(_ context.Context, pid, id, commentID, actorID string) (store.IssueCommentDeleteResult, error) {
	for index := range s.comments {
		comment := &s.comments[index]
		if pid != projectID || id != issueID || comment.ID != commentID {
			continue
		}
		hasReplies := false
		for _, candidate := range s.comments {
			if candidate.ParentCommentID != nil && *candidate.ParentCommentID == commentID {
				hasReplies = true
				break
			}
		}
		if hasReplies {
			at := comment.CreatedAt.Add(2 * time.Second)
			comment.Body = ""
			comment.DeletedAt = &at
			comment.ResolvedAt = nil
			comment.ResolvedByUserID = nil
			comment.Reactions = nil
		} else {
			s.comments = append(s.comments[:index], s.comments[index+1:]...)
		}
		event := store.Event{ID: "bcbcbcbc-bcbc-4bcb-8bcb-bcbcbcbcbcbc", Type: "issue.comment_changed", ProjectID: pid, IssueID: &id}
		return store.IssueCommentDeleteResult{Events: []store.Event{event}}, nil
	}
	return store.IssueCommentDeleteResult{}, store.ErrNotFound
}
func (s *issueCommentHTTPStore) ResolveIssueComment(_ context.Context, pid, id, commentID, actorID string) (store.IssueCommentMutationResult, error) {
	for index := range s.comments {
		comment := &s.comments[index]
		if pid == projectID && id == issueID && comment.ID == commentID {
			at := comment.CreatedAt.Add(3 * time.Second)
			comment.ResolvedAt = &at
			comment.ResolvedByUserID = &actorID
			comment.ResolvedByName = "Member"
			event := store.Event{ID: "cdcdcdcd-cdcd-4cdc-8dcd-cdcdcdcdcdcd", Type: "issue.comment_changed", ProjectID: pid, IssueID: &id}
			return store.IssueCommentMutationResult{Comment: *comment, Events: []store.Event{event}}, nil
		}
	}
	return store.IssueCommentMutationResult{}, store.ErrNotFound
}
func (s *issueCommentHTTPStore) ReopenIssueComment(_ context.Context, pid, id, commentID, actorID string) (store.IssueCommentMutationResult, error) {
	for index := range s.comments {
		comment := &s.comments[index]
		if pid == projectID && id == issueID && comment.ID == commentID {
			comment.ResolvedAt = nil
			comment.ResolvedByUserID = nil
			comment.ResolvedByName = ""
			event := store.Event{ID: "dededede-dede-4ded-8ded-dededededede", Type: "issue.comment_changed", ProjectID: pid, IssueID: &id}
			return store.IssueCommentMutationResult{Comment: *comment, Events: []store.Event{event}}, nil
		}
	}
	return store.IssueCommentMutationResult{}, store.ErrNotFound
}
func (s *issueCommentHTTPStore) AddIssueCommentReaction(_ context.Context, pid, id, commentID, actorID, reaction string) ([]store.Event, error) {
	for index := range s.comments {
		comment := &s.comments[index]
		if pid != projectID || id != issueID || comment.ID != commentID {
			continue
		}
		for summaryIndex := range comment.Reactions {
			summary := &comment.Reactions[summaryIndex]
			if summary.Reaction == reaction {
				for _, existing := range summary.ActorIDs {
					if existing == actorID {
						return nil, nil
					}
				}
				summary.Count++
				summary.ActorIDs = append(summary.ActorIDs, actorID)
				return []store.Event{{ID: "efefefef-efef-4efe-8efe-efefefefefef", Type: "issue.comment_changed", ProjectID: pid, IssueID: &id}}, nil
			}
		}
		comment.Reactions = append(comment.Reactions, store.IssueCommentReactionSummary{Reaction: reaction, Count: 1, ActorIDs: []string{actorID}})
		return []store.Event{{ID: "efefefef-efef-4efe-8efe-efefefefefef", Type: "issue.comment_changed", ProjectID: pid, IssueID: &id}}, nil
	}
	return nil, store.ErrNotFound
}
func (s *issueCommentHTTPStore) RemoveIssueCommentReaction(_ context.Context, pid, id, commentID, actorID, reaction string) ([]store.Event, error) {
	for index := range s.comments {
		comment := &s.comments[index]
		if pid != projectID || id != issueID || comment.ID != commentID {
			continue
		}
		for summaryIndex := range comment.Reactions {
			summary := &comment.Reactions[summaryIndex]
			if summary.Reaction != reaction {
				continue
			}
			for actorIndex, existing := range summary.ActorIDs {
				if existing == actorID {
					summary.ActorIDs = append(summary.ActorIDs[:actorIndex], summary.ActorIDs[actorIndex+1:]...)
					summary.Count--
					if summary.Count == 0 {
						comment.Reactions = append(comment.Reactions[:summaryIndex], comment.Reactions[summaryIndex+1:]...)
					}
					return []store.Event{{ID: "fafafafa-fafa-4afa-8afa-fafafafafafa", Type: "issue.comment_changed", ProjectID: pid, IssueID: &id}}, nil
				}
			}
		}
		return nil, nil
	}
	return nil, store.ErrNotFound
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
	if root.ID != httpCommentID || root.IssueID != issueKey || root.ParentCommentID != nil || root.SourceRunID != nil || root.Author.Type != store.ActorTypeHuman || root.Author.ID != member.ID {
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

	badCreateProject := httptest.NewRecorder()
	router.ServeHTTP(badCreateProject, httptest.NewRequest(http.MethodPost, "/api/projects/not-a-uuid/issues/"+issueKey+"/comments", strings.NewReader(`{"body":"bad project"}`)))
	if badCreateProject.Code != http.StatusBadRequest {
		t.Fatalf("bad create project status=%d body=%s", badCreateProject.Code, badCreateProject.Body.String())
	}
	for name, suffix := range map[string]string{
		"comments": "/comments",
		"timeline": "/timeline",
	} {
		t.Run("invalid issue key "+name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/issues/not-an-issue-key"+suffix, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	badCreateIssueKey := httptest.NewRecorder()
	router.ServeHTTP(badCreateIssueKey, httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/issues/not-an-issue-key/comments", strings.NewReader(`{"body":"bad issue"}`)))
	if badCreateIssueKey.Code != http.StatusBadRequest {
		t.Fatalf("bad create issue status=%d body=%s", badCreateIssueKey.Code, badCreateIssueKey.Body.String())
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


func TestIssueCommentHTTPLifecycleResolutionAndReactions(t *testing.T) {
	fixture, database := newIssueCommentHTTPFixture(t)
	author, authorToken := fixture.createUser(t, "lifecycle-author", store.DeploymentRoleMember)
	member, memberToken := fixture.createUser(t, "lifecycle-member", store.DeploymentRoleMember)
	viewer, viewerToken := fixture.createUser(t, "lifecycle-viewer", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, author.ID)] = store.ProjectRoleMember
	fixture.access.roles[projectGrantKey(projectID, member.ID)] = store.ProjectRoleMember
	fixture.access.roles[projectGrantKey(projectID, viewer.ID)] = store.ProjectRoleViewer

	at := time.Date(2026, 9, 19, 7, 0, 0, 0, time.UTC)
	parentID := httpCommentID
	database.comments = []store.IssueComment{
		{ID: httpCommentID, IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, AuthorName: "Author", Body: "root", CreatedAt: at, UpdatedAt: at},
		{ID: httpReplyID, IssueID: issueID, ParentCommentID: &parentID, AuthorType: store.ActorTypeHuman, AuthorID: member.ID, AuthorName: "Member", Body: "reply", CreatedAt: at.Add(time.Second), UpdatedAt: at.Add(time.Second)},
	}

	base := "/api/projects/" + projectID + "/issues/" + issueKey + "/comments/" + httpCommentID
	forbiddenEdit := authHTTPRequest(t, fixture.handler, http.MethodPatch, base, `{"body":"nope"}`, bearer(memberToken))
	if forbiddenEdit.Code != http.StatusForbidden {
		t.Fatalf("non-author edit status=%d body=%s", forbiddenEdit.Code, forbiddenEdit.Body.String())
	}
	forbiddenDelete := authHTTPRequest(t, fixture.handler, http.MethodDelete, base, "", bearer(memberToken))
	if forbiddenDelete.Code != http.StatusForbidden {
		t.Fatalf("non-author delete status=%d body=%s", forbiddenDelete.Code, forbiddenDelete.Body.String())
	}

	edited := authHTTPRequest(t, fixture.handler, http.MethodPatch, base, `{"body":"edited root"}`, bearer(authorToken))
	if edited.Code != http.StatusOK {
		t.Fatalf("edit status=%d body=%s", edited.Code, edited.Body.String())
	}
	var editedComment IssueCommentDTO
	if err := json.Unmarshal(edited.Body.Bytes(), &editedComment); err != nil {
		t.Fatal(err)
	}
	if editedComment.Body == nil || *editedComment.Body != "edited root" || !editedComment.UpdatedAt.After(editedComment.CreatedAt) {
		t.Fatalf("edited comment=%+v", editedComment)
	}

	viewerResolve := authHTTPRequest(t, fixture.handler, http.MethodPut, base+"/resolution", "", bearer(viewerToken))
	if viewerResolve.Code != http.StatusForbidden {
		t.Fatalf("viewer resolve status=%d body=%s", viewerResolve.Code, viewerResolve.Body.String())
	}
	resolved := authHTTPRequest(t, fixture.handler, http.MethodPut, base+"/resolution", "", bearer(memberToken))
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resolved.Code, resolved.Body.String())
	}
	var resolvedComment IssueCommentDTO
	if err := json.Unmarshal(resolved.Body.Bytes(), &resolvedComment); err != nil {
		t.Fatal(err)
	}
	if resolvedComment.ResolvedAt == nil || resolvedComment.ResolvedBy == nil || resolvedComment.ResolvedBy.ID != member.ID {
		t.Fatalf("resolved comment=%+v", resolvedComment)
	}
	replyResolve := authHTTPRequest(t, fixture.handler, http.MethodPut, "/api/projects/"+projectID+"/issues/"+issueKey+"/comments/"+httpReplyID+"/resolution", "", bearer(memberToken))
	if replyResolve.Code != http.StatusBadRequest {
		t.Fatalf("reply resolve status=%d body=%s", replyResolve.Code, replyResolve.Body.String())
	}
	reopened := authHTTPRequest(t, fixture.handler, http.MethodDelete, base+"/resolution", "", bearer(authorToken))
	if reopened.Code != http.StatusOK {
		t.Fatalf("reopen status=%d body=%s", reopened.Code, reopened.Body.String())
	}

	reactionPath := base + "/reactions/" + store.IssueCommentReactionHeart
	for i := 0; i < 2; i++ {
		response := authHTTPRequest(t, fixture.handler, http.MethodPut, reactionPath, "", bearer(memberToken))
		if response.Code != http.StatusNoContent {
			t.Fatalf("add reaction %d status=%d body=%s", i, response.Code, response.Body.String())
		}
	}
	memberList := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID+"/issues/"+issueKey+"/comments", "", bearer(memberToken))
	var memberComments []IssueCommentDTO
	if err := json.Unmarshal(memberList.Body.Bytes(), &memberComments); err != nil {
		t.Fatal(err)
	}
	if len(memberComments) != 2 || len(memberComments[0].Reactions) != 1 || memberComments[0].Reactions[0].Count != 1 || !memberComments[0].Reactions[0].ReactedByCurrentUser {
		t.Fatalf("member reactions=%+v", memberComments)
	}
	viewerList := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID+"/issues/"+issueKey+"/comments", "", bearer(viewerToken))
	var viewerComments []IssueCommentDTO
	if err := json.Unmarshal(viewerList.Body.Bytes(), &viewerComments); err != nil {
		t.Fatal(err)
	}
	if len(viewerComments[0].Reactions) != 1 || viewerComments[0].Reactions[0].ReactedByCurrentUser {
		t.Fatalf("viewer reactions=%+v", viewerComments)
	}
	for i := 0; i < 2; i++ {
		response := authHTTPRequest(t, fixture.handler, http.MethodDelete, reactionPath, "", bearer(memberToken))
		if response.Code != http.StatusNoContent {
			t.Fatalf("remove reaction %d status=%d body=%s", i, response.Code, response.Body.String())
		}
	}
	badReaction := authHTTPRequest(t, fixture.handler, http.MethodPut, base+"/reactions/PARTY", "", bearer(memberToken))
	if badReaction.Code != http.StatusBadRequest {
		t.Fatalf("bad reaction status=%d body=%s", badReaction.Code, badReaction.Body.String())
	}

	deleted := authHTTPRequest(t, fixture.handler, http.MethodDelete, base, "", bearer(authorToken))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	afterDelete := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID+"/issues/"+issueKey+"/comments", "", bearer(viewerToken))
	var deletedComments []IssueCommentDTO
	if err := json.Unmarshal(afterDelete.Body.Bytes(), &deletedComments); err != nil {
		t.Fatal(err)
	}
	if len(deletedComments) != 2 || deletedComments[0].Body != nil || deletedComments[0].DeletedAt == nil || deletedComments[1].ParentCommentID == nil || *deletedComments[1].ParentCommentID != httpCommentID {
		t.Fatalf("deleted projection=%+v", deletedComments)
	}
	repeatedDelete := authHTTPRequest(t, fixture.handler, http.MethodDelete, base, "", bearer(authorToken))
	if repeatedDelete.Code != http.StatusNoContent {
		t.Fatalf("repeated author delete status=%d body=%s", repeatedDelete.Code, repeatedDelete.Body.String())
	}
	forbiddenTombstoneDelete := authHTTPRequest(t, fixture.handler, http.MethodDelete, base, "", bearer(memberToken))
	if forbiddenTombstoneDelete.Code != http.StatusForbidden {
		t.Fatalf("non-author tombstone delete status=%d body=%s", forbiddenTombstoneDelete.Code, forbiddenTombstoneDelete.Body.String())
	}

	badID := authHTTPRequest(t, fixture.handler, http.MethodPatch, "/api/projects/"+projectID+"/issues/"+issueKey+"/comments/not-a-uuid", `{"body":"bad"}`, bearer(authorToken))
	if badID.Code != http.StatusBadRequest {
		t.Fatalf("bad comment id status=%d body=%s", badID.Code, badID.Body.String())
	}
	_, outsideToken := fixture.createUser(t, "lifecycle-outside", store.DeploymentRoleMember)
	hidden := authHTTPRequest(t, fixture.handler, http.MethodPut, base+"/resolution", "", bearer(outsideToken))
	if hidden.Code != http.StatusNotFound {
		t.Fatalf("outside mutation status=%d body=%s", hidden.Code, hidden.Body.String())
	}
}

func TestIssueCommentLifecycleLowLevelWritesFailClosed(t *testing.T) {
	database := &issueCommentHTTPStore{fakeControlPlaneStore: &fakeControlPlaneStore{}}
	control := app.New(database)
	router := NewRouter(control)
	base := "/api/projects/" + projectID + "/issues/" + issueKey + "/comments/" + httpCommentID

	for name, request := range map[string]*http.Request{
		"edit":     httptest.NewRequest(http.MethodPatch, base, strings.NewReader(`{"body":"edit"}`)),
		"delete":   httptest.NewRequest(http.MethodDelete, base, nil),
		"resolve":  httptest.NewRequest(http.MethodPut, base+"/resolution", nil),
		"reopen":   httptest.NewRequest(http.MethodDelete, base+"/resolution", nil),
		"react":    httptest.NewRequest(http.MethodPut, base+"/reactions/"+store.IssueCommentReactionHeart, nil),
		"unreact":  httptest.NewRequest(http.MethodDelete, base+"/reactions/"+store.IssueCommentReactionHeart, nil),
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
