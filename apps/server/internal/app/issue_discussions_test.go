package app

import (
	"context"
	"encoding/base64"
	"strings"
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
	rootErr       error
	threadErr     error
	updateErr     error
}

func (s *issueDiscussionTestStore) ListIssueDiscussionRoots(_ context.Context, _, _ string, limit int) ([]store.IssueDiscussionRoot, error) {
	s.rootLimit = limit
	if s.rootErr != nil {
		return nil, s.rootErr
	}
	return append([]store.IssueDiscussionRoot(nil), s.roots...), nil
}

func (s *issueDiscussionTestStore) GetIssueDiscussionThread(_ context.Context, _, _, _ string, depth, limit int) (store.IssueDiscussionThread, error) {
	s.threadDepth, s.threadLimit = depth, limit
	if s.threadErr != nil {
		return store.IssueDiscussionThread{}, s.threadErr
	}
	return s.thread, nil
}

func (s *issueDiscussionTestStore) ListIssueDiscussionUpdates(_ context.Context, _, _ string, cursor *store.IssueCommentCursor, limit, contextLimit, depth int) (store.IssueDiscussionUpdates, error) {
	s.cursor, s.updateLimit, s.updateContext, s.updateDepth = cursor, limit, contextLimit, depth
	if s.updateErr != nil {
		return store.IssueDiscussionUpdates{}, s.updateErr
	}
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
	viewer := AuthenticatedUser{ID: "viewer", Status: store.UserStatusActive, DeploymentRole: store.DeploymentRoleMember}
	if _, err := access.ListRecentIssueDiscussions(t.Context(), viewer, projectID, issueID, 1); err != nil {
		t.Fatalf("viewer recent discussion read error=%v", err)
	}
	if _, err := access.GetIssueDiscussionThread(t.Context(), viewer, projectID, issueID, "33333333-3333-4333-8333-333333333333", 1); err != nil {
		t.Fatalf("viewer thread discussion read error=%v", err)
	}
	if _, err := access.ListIssueDiscussionUpdates(t.Context(), viewer, projectID, issueID, "", 1); err != nil {
		t.Fatalf("viewer update discussion read error=%v", err)
	}
}

func TestIssueDiscussionApplicationHandlesUnavailableAndStoreFailures(t *testing.T) {
	const projectID = "project-1"
	const issueID = "11111111-1111-4111-8111-111111111111"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues:  map[string]store.Issue{issueID: {ID: issueID, ProjectID: projectID, Title: "Issue"}},
		runs:    map[string]store.Run{},
	}
	commentStore := &issueCommentTestStore{projectWorkflowAuthorizationStore: base}

	withoutDiscussions := New(commentStore)
	if _, err := withoutDiscussions.ListRecentIssueDiscussions(t.Context(), projectID, issueID, 1); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("recent unavailable error=%v", err)
	}
	if _, err := withoutDiscussions.GetIssueDiscussionThread(t.Context(), projectID, issueID, "33333333-3333-4333-8333-333333333333", 1); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("thread unavailable error=%v", err)
	}
	if _, err := withoutDiscussions.ListIssueDiscussionUpdates(t.Context(), projectID, issueID, "", 1); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("updates unavailable error=%v", err)
	}

	fake := &issueDiscussionTestStore{
		issueCommentTestStore: commentStore,
		rootErr:               store.ErrNotFound,
		threadErr:             store.ErrInvalidArgument,
		updateErr:             store.ErrNotFound,
	}
	service := New(fake)
	if _, err := service.ListRecentIssueDiscussions(t.Context(), projectID, issueID, 1); err == nil {
		t.Fatal("recent store failure unexpectedly succeeded")
	}
	if _, err := service.GetIssueDiscussionThread(t.Context(), projectID, issueID, "33333333-3333-4333-8333-333333333333", 1); err == nil {
		t.Fatal("thread store failure unexpectedly succeeded")
	}
	if _, err := service.ListIssueDiscussionUpdates(t.Context(), projectID, issueID, "", 1); err == nil {
		t.Fatal("updates store failure unexpectedly succeeded")
	}
	if _, err := service.ListRecentIssueDiscussions(t.Context(), projectID, "22222222-2222-4222-8222-222222222222", 1); err == nil {
		t.Fatal("missing Issue unexpectedly produced discussion roots")
	}

	if _, err := service.GetIssueDiscussionThread(t.Context(), projectID, issueID, "33333333-3333-4333-8333-333333333333", -1); err == nil {
		t.Fatal("negative thread limit unexpectedly succeeded")
	}
	if _, err := service.ListIssueDiscussionUpdates(t.Context(), projectID, issueID, "", -1); err == nil {
		t.Fatal("negative update limit unexpectedly succeeded")
	}
	missingIssueID := "22222222-2222-4222-8222-222222222222"
	if _, err := service.GetIssueDiscussionThread(t.Context(), projectID, missingIssueID, "33333333-3333-4333-8333-333333333333", 1); err == nil {
		t.Fatal("missing Issue unexpectedly produced a thread")
	}
	if _, err := service.ListIssueDiscussionUpdates(t.Context(), projectID, missingIssueID, "", 1); err == nil {
		t.Fatal("missing Issue unexpectedly produced updates")
	}
	fake.updateErr = nil
	fake.updates = store.IssueDiscussionUpdates{NextCursor: &store.IssueCommentCursor{
		CreatedAt: time.Now(),
		ID:        "not-a-uuid",
	}}
	if _, err := service.ListIssueDiscussionUpdates(t.Context(), projectID, issueID, "", 1); err == nil {
		t.Fatal("invalid store cursor unexpectedly encoded")
	}


	if _, err := encodeIssueDiscussionCursor(store.IssueCommentCursor{}); err == nil {
		t.Fatal("zero cursor unexpectedly encoded")
	}
	if _, err := encodeIssueDiscussionCursor(store.IssueCommentCursor{CreatedAt: time.Now(), ID: "not-a-uuid"}); err == nil {
		t.Fatal("invalid cursor id unexpectedly encoded")
	}
	if _, err := decodeIssueDiscussionCursor("e30"); err == nil {
		t.Fatal("incomplete cursor payload unexpectedly decoded")
	}
}

func TestProductionServicesPreserveIssueDiscussionCapability(t *testing.T) {
	const projectID = "project-1"
	const issueID = "11111111-1111-4111-8111-111111111111"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues:  map[string]store.Issue{issueID: {ID: issueID, ProjectID: projectID, Title: "Issue"}},
		runs:    map[string]store.Run{},
	}
	fake := &issueDiscussionTestStore{
		issueCommentTestStore: &issueCommentTestStore{projectWorkflowAuthorizationStore: base},
		roots: []store.IssueDiscussionRoot{{
			Root: store.IssueComment{ID: "root", IssueID: issueID},
		}},
	}
	services, err := NewServicesWithRuntimes(fake, workspaceMaterializerFunc(func(_ context.Context, _ store.Project, _ store.Issue, workspace store.Workspace) (store.Workspace, error) {
		return workspace, nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = services.Close() })

	if services.ControlPlane.issueDiscussions != fake {
		t.Fatal("Issue discussion reads were not bound to the authoritative control-plane store")
	}
	roots, err := services.ControlPlane.ListRecentIssueDiscussions(t.Context(), projectID, issueID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Root.ID != "root" || fake.rootLimit != 1 {
		t.Fatalf("roots=%+v limit=%d", roots, fake.rootLimit)
	}
}

func TestIssueDiscussionCursorEncodingIsStrictAndRoundTrips(t *testing.T) {
	position := store.IssueCommentCursor{
		CreatedAt: time.Date(2026, 9, 20, 9, 30, 0, 123, time.FixedZone("test", 2*60*60)),
		ID:        "ABCDEFAB-CDEF-4ABC-8DEF-ABCDEFABCDEF",
	}
	encoded, err := encodeIssueDiscussionCursor(position)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeIssueDiscussionCursor("  " + encoded + "  ")
	if err != nil {
		t.Fatal(err)
	}
	if decoded == nil || decoded.ID != position.ID || !decoded.CreatedAt.Equal(position.CreatedAt) || decoded.CreatedAt.Location() != time.UTC {
		t.Fatalf("decoded cursor=%+v", decoded)
	}

	for name, raw := range map[string]string{
		"invalid base64": "***",
		"unknown field": base64.RawURLEncoding.EncodeToString([]byte(`{"createdAt":"2026-09-20T09:30:00Z","id":"11111111-1111-4111-8111-111111111111","extra":true}`)),
		"trailing value": base64.RawURLEncoding.EncodeToString([]byte(`{"createdAt":"2026-09-20T09:30:00Z","id":"11111111-1111-4111-8111-111111111111"} {}`)),
		"invalid hex id": base64.RawURLEncoding.EncodeToString([]byte(`{"createdAt":"2026-09-20T09:30:00Z","id":"11111111-1111-4111-8111-11111111111g"}`)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeIssueDiscussionCursor(raw); err == nil {
				t.Fatalf("invalid cursor %q unexpectedly decoded", raw)
			}
		})
	}
}
