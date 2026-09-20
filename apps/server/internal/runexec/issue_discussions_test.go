package runexec

import (
	"context"
	"errors"
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
	recentErr error
	threadErr error
	updateErr error
}

func (s *recordingIssueDiscussionService) ListRecentIssueDiscussions(_ context.Context, projectID, issueID string, limit int) ([]store.IssueDiscussionRoot, error) {
	s.projectID, s.issueID, s.limit = projectID, issueID, limit
	if s.recentErr != nil {
		return nil, s.recentErr
	}
	at := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)
	root := store.IssueComment{ID: "root", AuthorType: store.ActorTypeAgent, AuthorID: "agent", AuthorName: "Agent", Body: "finding", SourceRunID: stringPointer("run-1"), SourceActionKey: stringPointer("internal-action"), CreatedAt: at, UpdatedAt: at}
	return []store.IssueDiscussionRoot{{
		Root: root, ReplyCount: 2, LastActivityAt: at, CompactComments: []store.IssueComment{root},
	}}, nil
}

func (s *recordingIssueDiscussionService) GetIssueDiscussionThread(_ context.Context, projectID, issueID, anchorID string, limit int) (store.IssueDiscussionThread, error) {
	s.projectID, s.issueID, s.anchorID, s.limit = projectID, issueID, anchorID, limit
	if s.threadErr != nil {
		return store.IssueDiscussionThread{}, s.threadErr
	}
	at := time.Date(2026, 9, 20, 11, 5, 0, 0, time.UTC)
	return store.IssueDiscussionThread{
		RootID: "root", AnchorID: anchorID,
		Comments: []store.IssueComment{{
			ID: "thread-comment", AuthorType: store.ActorTypeHuman, AuthorID: "user-1", AuthorName: "User",
			Body: "thread context", CreatedAt: at, UpdatedAt: at,
		}},
	}, nil
}

func (s *recordingIssueDiscussionService) ListIssueDiscussionUpdates(_ context.Context, projectID, issueID, cursor string, limit int) (app.IssueDiscussionUpdatePage, error) {
	s.projectID, s.issueID, s.cursor, s.limit = projectID, issueID, cursor, limit
	if s.updateErr != nil {
		return app.IssueDiscussionUpdatePage{}, s.updateErr
	}
	at := time.Date(2026, 9, 20, 11, 10, 0, 0, time.UTC)
	return app.IssueDiscussionUpdatePage{
		Comments: []store.IssueDiscussionComment{{
			Comment: store.IssueComment{
				ID: "update-comment", AuthorType: store.ActorTypeHuman, AuthorID: "user-1", AuthorName: "User",
				Body: "update context", CreatedAt: at, UpdatedAt: at,
			},
			IsNew: true, ContextTruncated: true,
		}},
		NextCursor: "next", HasMore: true,
	}, nil
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
	if len(thread.Thread.Comments) != 1 || thread.Thread.Comments[0].Body == nil || *thread.Thread.Comments[0].Body != "thread context" {
		t.Fatalf("mapped thread comments=%+v", thread.Thread.Comments)
	}
	updates, err := reader.ReadIssueDiscussion(t.Context(), engine.IssueDiscussionReadRequest{Mode: engine.IssueDiscussionReadUpdates, Cursor: "cursor-1"})
	if err != nil || updates.Updates == nil || updates.Updates.NextCursor != "next" || service.cursor != "cursor-1" {
		t.Fatalf("updates=%+v service=%+v err=%v", updates, service, err)
	}
	if len(updates.Updates.Comments) != 1 || !updates.Updates.Comments[0].IsNew || !updates.Updates.Comments[0].ContextTruncated ||
		updates.Updates.Comments[0].Comment.Body == nil || *updates.Updates.Comments[0].Comment.Body != "update context" {
		t.Fatalf("mapped updates=%+v", updates.Updates.Comments)
	}
	if _, err := reader.ReadIssueDiscussion(t.Context(), engine.IssueDiscussionReadRequest{Mode: engine.IssueDiscussionReadThread}); err == nil {
		t.Fatal("thread without anchor unexpectedly succeeded")
	}
	if _, err := reader.ReadIssueDiscussion(t.Context(), engine.IssueDiscussionReadRequest{Mode: "unknown"}); err == nil {
		t.Fatal("unknown read mode unexpectedly succeeded")
	}
}

func stringPointer(value string) *string { return &value }

func TestIssueDiscussionReaderPropagatesSharedQueryFailures(t *testing.T) {
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Issue:   executioncontext.IssueContext{ID: "issue-1"},
	}
	var nilReader *issueDiscussionReader
	if _, err := nilReader.ReadIssueDiscussion(t.Context(), engine.IssueDiscussionReadRequest{Mode: engine.IssueDiscussionReadRecent}); err == nil {
		t.Fatal("nil discussion reader unexpectedly succeeded")
	}

	want := errors.New("shared discussion read failed")
	service := &recordingIssueDiscussionService{recentErr: want, threadErr: want, updateErr: want}
	reader := newIssueDiscussionReader(service, safe)
	for _, request := range []engine.IssueDiscussionReadRequest{
		{Mode: engine.IssueDiscussionReadRecent},
		{Mode: engine.IssueDiscussionReadThread, AnchorCommentID: "comment-1"},
		{Mode: engine.IssueDiscussionReadUpdates, Cursor: "cursor-1"},
	} {
		if _, err := reader.ReadIssueDiscussion(t.Context(), request); !errors.Is(err, want) {
			t.Fatalf("request=%+v err=%v", request, err)
		}
	}
}

func TestMapEngineIssueDiscussionCommentPreservesPublicContextAndTombstonesBody(t *testing.T) {
	at := time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC)
	parentID := "parent-1"
	runID := "run-1"
	actionKey := "internal-action-key"
	resolverID := "user-1"
	value := store.IssueComment{
		ID:               "comment-1",
		IssueID:          "issue-1",
		ParentCommentID:  &parentID,
		AuthorType:       store.ActorTypeAgent,
		AuthorID:         "agent-1",
		AuthorName:       "Agent",
		SourceRunID:      &runID,
		SourceActionKey:  &actionKey,
		Body:             "must stay hidden after deletion",
		DeletedAt:        &at,
		ResolvedAt:       &at,
		ResolvedByUserID: &resolverID,
		ResolvedByName:   "Resolver",
		Reactions: []store.IssueCommentReactionSummary{
			{Reaction: store.IssueCommentReactionHeart, Count: 2, ActorIDs: []string{"user-1", "user-2"}},
		},
		CreatedAt: at.Add(-time.Minute),
		UpdatedAt: at,
	}
	mapped := mapEngineIssueDiscussionComment(value)
	if mapped.Body != nil || mapped.ParentCommentID == nil || *mapped.ParentCommentID != parentID ||
		mapped.SourceRunID == nil || *mapped.SourceRunID != runID || mapped.ResolvedBy == nil ||
		mapped.ResolvedBy.ID != resolverID || mapped.ResolvedBy.Name != "Resolver" ||
		len(mapped.Reactions) != 1 || mapped.Reactions[0].Reaction != store.IssueCommentReactionHeart || mapped.Reactions[0].Count != 2 {
		t.Fatalf("mapped=%+v", mapped)
	}
}
