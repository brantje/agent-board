package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *issueCommentHTTPStore) ListIssueDiscussionRoots(_ context.Context, pid, id string, limit int) ([]store.IssueDiscussionRoot, error) {
	if pid != projectID || id != issueID {
		return nil, store.ErrNotFound
	}
	if len(s.comments) == 0 {
		return []store.IssueDiscussionRoot{}, nil
	}
	root := s.comments[0]
	last := root.CreatedAt
	replies := 0
	for _, comment := range s.comments[1:] {
		if comment.CreatedAt.After(last) {
			last = comment.CreatedAt
		}
		replies++
	}
	compact := []store.IssueComment{}
	if root.ResolvedAt != nil {
		compact = append(compact, s.comments...)
	}
	return []store.IssueDiscussionRoot{{Root: root, ReplyCount: replies, LastActivityAt: last, CompactComments: compact}}, nil
}

func (s *issueCommentHTTPStore) GetIssueDiscussionThread(_ context.Context, pid, id, anchorID string, _, _ int) (store.IssueDiscussionThread, error) {
	if pid != projectID || id != issueID {
		return store.IssueDiscussionThread{}, store.ErrNotFound
	}
	found := false
	for _, comment := range s.comments {
		if comment.ID == anchorID {
			found = true
			break
		}
	}
	if !found || len(s.comments) == 0 {
		return store.IssueDiscussionThread{}, store.ErrNotFound
	}
	return store.IssueDiscussionThread{RootID: s.comments[0].ID, AnchorID: anchorID, Comments: append([]store.IssueComment(nil), s.comments...)}, nil
}

func (s *issueCommentHTTPStore) ListIssueDiscussionUpdates(_ context.Context, pid, id string, _ *store.IssueCommentCursor, _, _, _ int) (store.IssueDiscussionUpdates, error) {
	if pid != projectID || id != issueID {
		return store.IssueDiscussionUpdates{}, store.ErrNotFound
	}
	items := make([]store.IssueDiscussionComment, 0, len(s.comments))
	for _, comment := range s.comments {
		items = append(items, store.IssueDiscussionComment{Comment: comment, IsNew: true})
	}
	if len(s.comments) == 0 {
		return store.IssueDiscussionUpdates{Comments: items}, nil
	}
	last := s.comments[len(s.comments)-1]
	return store.IssueDiscussionUpdates{
		Comments:   items,
		NextCursor: &store.IssueCommentCursor{CreatedAt: last.CreatedAt, ID: last.ID},
	}, nil
}

func TestIssueDiscussionHTTPReadsUseCanonicalProjection(t *testing.T) {
	fixture, database := newIssueCommentHTTPFixture(t)
	viewer, token := fixture.createUser(t, "discussion-viewer", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, viewer.ID)] = store.ProjectRoleViewer

	rootAt := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	rootID := "11111111-1111-4111-8111-111111111111"
	replyID := "22222222-2222-4222-8222-222222222222"
	database.comments = []store.IssueComment{
		{ID: rootID, IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: viewer.ID, AuthorName: "Viewer", Body: "root", CreatedAt: rootAt, UpdatedAt: rootAt},
		{ID: replyID, IssueID: issueID, ParentCommentID: &rootID, AuthorType: store.ActorTypeHuman, AuthorID: viewer.ID, AuthorName: "Viewer", Body: "reply", CreatedAt: rootAt.Add(time.Second), UpdatedAt: rootAt.Add(time.Second)},
	}

	base := "/api/projects/" + projectID + "/issues/" + issueKey + "/comments"
	rootsResponse := authHTTPRequest(t, fixture.handler, http.MethodGet, base+"/discussions?limit=5", "", bearer(token))
	if rootsResponse.Code != http.StatusOK {
		t.Fatalf("roots status=%d body=%s", rootsResponse.Code, rootsResponse.Body.String())
	}
	var roots []IssueDiscussionRootDTO
	if err := json.Unmarshal(rootsResponse.Body.Bytes(), &roots); err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Root.ID != rootID || roots[0].Root.IssueID != issueKey || roots[0].ReplyCount != 1 || len(roots[0].CompactComments) != 0 {
		t.Fatalf("roots=%+v", roots)
	}

	threadResponse := authHTTPRequest(t, fixture.handler, http.MethodGet, base+"/"+replyID+"/thread", "", bearer(token))
	if threadResponse.Code != http.StatusOK {
		t.Fatalf("thread status=%d body=%s", threadResponse.Code, threadResponse.Body.String())
	}
	var thread IssueDiscussionThreadDTO
	if err := json.Unmarshal(threadResponse.Body.Bytes(), &thread); err != nil {
		t.Fatal(err)
	}
	if thread.RootID != rootID || thread.AnchorID != replyID || len(thread.Comments) != 2 {
		t.Fatalf("thread=%+v", thread)
	}

	updatesResponse := authHTTPRequest(t, fixture.handler, http.MethodGet, base+"/updates", "", bearer(token))
	if updatesResponse.Code != http.StatusOK {
		t.Fatalf("updates status=%d body=%s", updatesResponse.Code, updatesResponse.Body.String())
	}
	var updates IssueDiscussionUpdatesDTO
	if err := json.Unmarshal(updatesResponse.Body.Bytes(), &updates); err != nil {
		t.Fatal(err)
	}
	if len(updates.Comments) != 2 || updates.NextCursor == nil || *updates.NextCursor == "" || !updates.Comments[1].IsNew {
		t.Fatalf("updates=%+v", updates)
	}

	badLimit := authHTTPRequest(t, fixture.handler, http.MethodGet, base+"/discussions?limit=nope", "", bearer(token))
	if badLimit.Code != http.StatusBadRequest {
		t.Fatalf("bad limit status=%d body=%s", badLimit.Code, badLimit.Body.String())
	}
	badCursor := authHTTPRequest(t, fixture.handler, http.MethodGet, base+"/updates?cursor=forged", "", bearer(token))
	if badCursor.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor status=%d body=%s", badCursor.Code, badCursor.Body.String())
	}
}
