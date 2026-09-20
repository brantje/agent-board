package app

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueDiscussionTestStore struct {
	*issueCommentTestStore
	roots         []store.IssueDiscussionRoot
	thread        store.IssueDiscussionThread
	updates       store.IssueDiscussionUpdates
	rootLimit     int
	threadLimit   int
	threadDepth   int
	updateLimit   int
	updateContext int
	updateDepth   int
	cursor        *store.IssueCommentCursor
}

func (s *issueDiscussionTestStore) ListIssueDiscussionRoots(_ context.Context, _, _ string, limit int) ([]store.IssueDiscussionRoot, error) {
	s.rootLimit = limit
	return append([]store.IssueDiscussionRoot(nil), s.roots...), nil
}

func (s *issueDiscussionTestStore) GetIssueDiscussionThread(_ context.Context, _, _, _ string, depth, limit int) (store.IssueDiscussionThread, error) {
	s.threadDepth, s.threadLimit = depth, limit
	return s.thread, nil
}

func (s *issueDiscussionTestStore) ListIssueDiscussionUpdates(_ context.Context, _, _ string, cursor *store.IssueCommentCursor, limit, contextLimit, depth int) (store.IssueDiscussionUpdates, error) {
	s.cursor, s.updateLimit, s.updateContext, s.updateDepth = cursor, limit, contextLimit, depth
	return s.updates, nil
}

func TestIssueDiscussionApplicationBoundsReadsAndKeepsOpaqueCursor(t *testing.T) {
	const projectID = "project-1"
	const issueID = "11111111-1111-4111-8111-111111111111"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		roles:   map[string]string{"viewer": store.ProjectRoleViewer},
		issues:  map[string]store.Issue{issueID: {ID: issueID, ProjectID: projectID, Title: "Issue"}},
		runs:    map[string]store.Run{},
	}
	commentStore := &issueCommentTestStore{projectWorkflowAuthorizationStore: base}
	cursorPosition := store.IssueCommentCursor{
		CreatedAt: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
		ID:        "22222222-2222-4222-8222-222222222222",
	}
	fake := &issueDiscussionTestStore{
		issueCommentTestStore: commentStore,
		roots: []store.IssueDiscussionRoot{{
			Root:           store.IssueComment{ID: "root", IssueID: issueID},
			ReplyCount:     2,
			LastActivityAt: cursorPosition.CreatedAt,
		}},
		thread: store.IssueDiscussionThread{RootID: "root", AnchorID: "33333333-3333-4333-8333-333333333333"},
		updates: store.IssueDiscussionUpdates{
			NextCursor: &cursorPosition,
			HasMore:    true,
		},
	}
	service := New(fake)

	roots, err := service.ListRecentIssueDiscussions(t.Context(), projectID, issueID, 999)
	if err != nil || len(roots) != 1 || fake.rootLimit != issueDiscussionMaxRootLimit {
		t.Fatalf("roots=%+v limit=%d err=%v", roots, fake.rootLimit, err)
	}
	thread, err := service.GetIssueDiscussionThread(t.Context(), projectID, issueID, "33333333-3333-4333-8333-333333333333", 999)
	if err != nil || thread.RootID != "root" || fake.threadLimit != issueDiscussionMaxThreadLimit || fake.threadDepth != issueDiscussionMaxDepth {
		t.Fatalf("thread=%+v limit=%d depth=%d err=%v", thread, fake.threadLimit, fake.threadDepth, err)
	}
	page, err := service.ListIssueDiscussionUpdates(t.Context(), projectID, issueID, "", 999)
	if err != nil || page.NextCursor == "" || !page.HasMore || fake.updateLimit != issueDiscussionMaxUpdateLimit || fake.updateContext != issueDiscussionContextLimit || fake.updateDepth != issueDiscussionMaxDepth {
		t.Fatalf("page=%+v limit=%d context=%d depth=%d err=%v", page, fake.updateLimit, fake.updateContext, fake.updateDepth, err)
	}

	fake.updates = store.IssueDiscussionUpdates{}
	second, err := service.ListIssueDiscussionUpdates(t.Context(), projectID, issueID, page.NextCursor, 0)
	if err != nil || second.NextCursor != page.NextCursor || fake.cursor == nil || fake.cursor.ID != cursorPosition.ID || !fake.cursor.CreatedAt.Equal(cursorPosition.CreatedAt) || fake.updateLimit != issueDiscussionDefaultUpdateLimit {
		t.Fatalf("second=%+v decoded=%+v limit=%d err=%v", second, fake.cursor, fake.updateLimit, err)
	}
	if _, err := service.ListIssueDiscussionUpdates(t.Context(), projectID, issueID, "not-a-cursor", 1); err == nil {
		t.Fatal("invalid cursor unexpectedly succeeded")
	}
	if _, err := service.GetIssueDiscussionThread(t.Context(), projectID, issueID, "not-a-uuid", 1); err == nil {
		t.Fatal("invalid thread anchor unexpectedly succeeded")
	}
	if _, err := service.ListRecentIssueDiscussions(t.Context(), projectID, issueID, -1); err == nil {
		t.Fatal("negative limit unexpectedly succeeded")
	}

	access, err := NewProjectAccessService(service, fake)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.ListRecentIssueDiscussions(t.Context(), AuthenticatedUser{ID: "viewer", Status: store.UserStatusActive, DeploymentRole: store.DeploymentRoleMember}, projectID, issueID, 1); err != nil {
		t.Fatalf("viewer discussion read error=%v", err)
	}
}
