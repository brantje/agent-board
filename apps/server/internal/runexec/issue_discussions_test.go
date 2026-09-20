package runexec

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type recordingIssueDiscussionService struct {
	projectID string
	issueID   string
	anchorID  string
	cursor    string
	limit     int
}

func (s *recordingIssueDiscussionService) ListRecentIssueDiscussions(_ context.Context, projectID, issueID string, limit int) ([]store.IssueDiscussionRoot, error) {
	s.projectID, s.issueID, s.limit = projectID, issueID, limit
	at := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)
	root := store.IssueComment{ID: "root", AuthorType: store.ActorTypeAgent, AuthorID: "agent", AuthorName: "Agent", Body: "finding", SourceRunID: stringPointer("run-1"), SourceActionKey: stringPointer("internal-action"), CreatedAt: at, UpdatedAt: at}
	return []store.IssueDiscussionRoot{{
		Root: root, ReplyCount: 2, LastActivityAt: at, CompactComments: []store.IssueComment{root},
	}}, nil
}

func (s *recordingIssueDiscussionService) GetIssueDiscussionThread(_ context.Context, projectID, issueID, anchorID string, limit int) (store.IssueDiscussionThread, error) {
	s.projectID, s.issueID, s.anchorID, s.limit = projectID, issueID, anchorID, limit
	return store.IssueDiscussionThread{RootID: "root", AnchorID: anchorID}, nil
}

func (s *recordingIssueDiscussionService) ListIssueDiscussionUpdates(_ context.Context, projectID, issueID, cursor string, limit int) (app.IssueDiscussionUpdatePage, error) {
	s.projectID, s.issueID, s.cursor, s.limit = projectID, issueID, cursor, limit
	return app.IssueDiscussionUpdatePage{NextCursor: "next", HasMore: true}, nil
}

func TestIssueDiscussionReaderUsesTrustedIssueScopeAndSharedQueries(t *testing.T) {
	service := &recordingIssueDiscussionService{}
	reader := newIssueDiscussionReader(service, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Issue: executioncontext.IssueContext{ID: "issue-1"},
	})

	recent, err := reader.ReadIssueDiscussion(t.Context(), engine.IssueDiscussionReadRequest{Mode: engine.IssueDiscussionReadRecent, Limit: 7})
	if err != nil || service.projectID != "project-1" || service.issueID != "issue-1" || service.limit != 7 || len(recent.Roots) != 1 {
		t.Fatalf("recent=%+v service=%+v err=%v", recent, service, err)
	}
	if recent.Roots[0].Root.SourceRunID == nil || *recent.Roots[0].Root.SourceRunID != "run-1" || recent.Roots[0].Root.Body == nil || *recent.Roots[0].Root.Body != "finding" {
		t.Fatalf("mapped root=%+v", recent.Roots[0].Root)
	}
	if len(recent.Roots[0].CompactComments) != 1 || recent.Roots[0].CompactComments[0].Body == nil || *recent.Roots[0].CompactComments[0].Body != "finding" {
		t.Fatalf("mapped compact discussion=%+v", recent.Roots[0].CompactComments)
	}

	thread, err := reader.ReadIssueDiscussion(t.Context(), engine.IssueDiscussionReadRequest{Mode: engine.IssueDiscussionReadThread, AnchorCommentID: "comment-1"})
	if err != nil || thread.Thread == nil || service.anchorID != "comment-1" || service.projectID != "project-1" || service.issueID != "issue-1" {
		t.Fatalf("thread=%+v service=%+v err=%v", thread, service, err)
	}
	updates, err := reader.ReadIssueDiscussion(t.Context(), engine.IssueDiscussionReadRequest{Mode: engine.IssueDiscussionReadUpdates, Cursor: "cursor-1"})
	if err != nil || updates.Updates == nil || updates.Updates.NextCursor != "next" || service.cursor != "cursor-1" {
		t.Fatalf("updates=%+v service=%+v err=%v", updates, service, err)
	}
	if _, err := reader.ReadIssueDiscussion(t.Context(), engine.IssueDiscussionReadRequest{Mode: engine.IssueDiscussionReadThread}); err == nil {
		t.Fatal("thread without anchor unexpectedly succeeded")
	}
	if _, err := reader.ReadIssueDiscussion(t.Context(), engine.IssueDiscussionReadRequest{Mode: "unknown"}); err == nil {
		t.Fatal("unknown read mode unexpectedly succeeded")
	}
}

func stringPointer(value string) *string { return &value }
