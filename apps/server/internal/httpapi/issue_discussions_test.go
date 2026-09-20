package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
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

func TestIssueDiscussionHTTPAuthorizationAndCompactProjection(t *testing.T) {
	fixture, database := newIssueCommentHTTPFixture(t)
	viewer, token := fixture.createUser(t, "discussion-compact-viewer", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, viewer.ID)] = store.ProjectRoleViewer

	base := "/api/projects/" + projectID + "/issues/" + issueKey + "/comments"
	for _, path := range []string{
		base + "/discussions",
		base + "/11111111-1111-4111-8111-111111111111/thread",
		base + "/updates",
	} {
		response := authHTTPRequest(t, fixture.handler, http.MethodGet, path, "", nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}

	resolvedAt := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	rootID := "11111111-1111-4111-8111-111111111111"
	replyID := "22222222-2222-4222-8222-222222222222"
	database.comments = []store.IssueComment{
		{
			ID: rootID, IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: viewer.ID, AuthorName: "Viewer",
			Body: "resolved root", ResolvedAt: &resolvedAt, ResolvedByUserID: &viewer.ID, ResolvedByName: "Viewer",
			CreatedAt: resolvedAt.Add(-time.Minute), UpdatedAt: resolvedAt,
		},
		{
			ID: replyID, IssueID: issueID, ParentCommentID: &rootID, AuthorType: store.ActorTypeHuman, AuthorID: viewer.ID, AuthorName: "Viewer",
			Body: "conclusion", CreatedAt: resolvedAt, UpdatedAt: resolvedAt,
		},
	}
	rootsResponse := authHTTPRequest(t, fixture.handler, http.MethodGet, base+"/discussions", "", bearer(token))
	if rootsResponse.Code != http.StatusOK {
		t.Fatalf("resolved roots status=%d body=%s", rootsResponse.Code, rootsResponse.Body.String())
	}
	var roots []IssueDiscussionRootDTO
	if err := json.Unmarshal(rootsResponse.Body.Bytes(), &roots); err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || len(roots[0].CompactComments) != 2 || roots[0].CompactComments[1].Body == nil || *roots[0].CompactComments[1].Body != "conclusion" {
		t.Fatalf("resolved roots=%+v", roots)
	}

	database.comments = nil
	updatesResponse := authHTTPRequest(t, fixture.handler, http.MethodGet, base+"/updates", "", bearer(token))
	if updatesResponse.Code != http.StatusOK {
		t.Fatalf("empty updates status=%d body=%s", updatesResponse.Code, updatesResponse.Body.String())
	}
	var updates IssueDiscussionUpdatesDTO
	if err := json.Unmarshal(updatesResponse.Body.Bytes(), &updates); err != nil {
		t.Fatal(err)
	}
	if updates.NextCursor != nil || len(updates.Comments) != 0 || updates.HasMore {
		t.Fatalf("empty updates=%+v", updates)
	}

	badAnchor := authHTTPRequest(t, fixture.handler, http.MethodGet, base+"/not-a-uuid/thread", "", bearer(token))
	if badAnchor.Code != http.StatusBadRequest {
		t.Fatalf("bad anchor status=%d body=%s", badAnchor.Code, badAnchor.Body.String())
	}
}

func TestIssueDiscussionHTTPFallbackErrorsAndPathValidation(t *testing.T) {
	fixture, database := newIssueCommentHTTPFixture(t)
	viewer, token := fixture.createUser(t, "discussion-fallback-viewer", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, viewer.ID)] = store.ProjectRoleViewer
	outsider, outsiderToken := fixture.createUser(t, "discussion-outsider", store.DeploymentRoleMember)

	at := time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC)
	rootID := "11111111-1111-4111-8111-111111111111"
	database.comments = []store.IssueComment{{
		ID: rootID, IssueID: issueID, AuthorType: store.ActorTypeHuman, AuthorID: viewer.ID,
		AuthorName: "Viewer", Body: "root", CreatedAt: at, UpdatedAt: at,
	}}
	base := "/api/projects/" + projectID + "/issues/" + issueKey + "/comments"

	for _, path := range []string{
		base + "/discussions",
		base + "/" + rootID + "/thread",
		base + "/updates",
	} {
		response := authHTTPRequest(t, fixture.handler, http.MethodGet, path, "", bearer(outsiderToken))
		if response.Code != http.StatusNotFound {
			t.Fatalf("outsider %s status=%d body=%s user=%s", path, response.Code, response.Body.String(), outsider.ID)
		}
	}

	missingThread := authHTTPRequest(t, fixture.handler, http.MethodGet, base+"/44444444-4444-4444-8444-444444444444/thread", "", bearer(token))
	if missingThread.Code != http.StatusNotFound {
		t.Fatalf("missing thread status=%d body=%s", missingThread.Code, missingThread.Body.String())
	}


	badProject := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/not-a-uuid/issues/"+issueKey+"/comments/discussions", "", bearer(token))
	if badProject.Code != http.StatusBadRequest {
		t.Fatalf("bad project status=%d body=%s", badProject.Code, badProject.Body.String())
	}
	badIssue := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID+"/issues/not-an-issue-key/comments/discussions", "", bearer(token))
	if badIssue.Code != http.StatusBadRequest {
		t.Fatalf("bad issue status=%d body=%s", badIssue.Code, badIssue.Body.String())
	}
}

func TestIssueDiscussionHTTPReadsWithoutProjectAccessAdapter(t *testing.T) {
	fixture, database := newIssueCommentHTTPFixture(t)
	_, token := fixture.createUser(t, "discussion-direct-reader", store.DeploymentRoleMember)

	at := time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC)
	rootID := "11111111-1111-4111-8111-111111111111"
	database.comments = []store.IssueComment{{
		ID: rootID, IssueID: issueID, AuthorType: store.ActorTypeHuman,
		AuthorID: "22222222-2222-4222-8222-222222222222", AuthorName: "Author",
		Body: "direct read", CreatedAt: at, UpdatedAt: at,
	}}

	control := app.New(database)
	fixture.handler = NewRouterWithApplication(&app.Services{ControlPlane: control, Auth: fixture.auth})

	base := "/api/projects/" + projectID + "/issues/" + issueKey + "/comments"
	for name, path := range map[string]string{
		"recent":  base + "/discussions",
		"thread":  base + "/" + rootID + "/thread",
		"updates": base + "/updates",
	} {
		t.Run(name, func(t *testing.T) {
			response := authHTTPRequest(t, fixture.handler, http.MethodGet, path, "", bearer(token))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
