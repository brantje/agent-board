package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueCommentsPersistRepliesIsolationOrderingAndNoExecutionSideEffects(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	author, err := s.CreateUser(ctx, authUser("comment-author", "comment-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, testProjectInput("Comments", "/repo/comments", "CMT"))
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := s.CreateProject(ctx, testProjectInput("Other comments", "/repo/other-comments", "OCM"))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Discussion", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Sibling", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.CreateIssue(ctx, store.Issue{ProjectID: otherProject.ID, Title: "Foreign", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}

	before, err := s.GetIssue(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeRuns, err := s.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var beforeExecutionEvents int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM events
		WHERE issue_id=$1 AND type = ANY (ARRAY['run.created','run.cancelled','run.resumed'])
	`, issue.ID).Scan(&beforeExecutionEvents); err != nil {
		t.Fatal(err)
	}

	rootResult, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "Root @nobody stays plain text",
	})
	if err != nil {
		t.Fatal(err)
	}
	root := rootResult.Comment
	if root.ID == "" || root.ParentCommentID != nil || root.AuthorID != author.ID || root.AuthorName != author.DisplayName {
		t.Fatalf("root=%+v", root)
	}
	if len(rootResult.Events) != 1 || rootResult.Events[0].Type != "issue.comment_created" {
		t.Fatalf("root events=%+v", rootResult.Events)
	}
	if string(rootResult.Events[0].Payload) == "" || strings.Contains(string(rootResult.Events[0].Payload), root.Body) {
		t.Fatalf("event payload unexpectedly contains comment body: %s", rootResult.Events[0].Payload)
	}

	var actorPayload map[string]string
	if err := json.Unmarshal(rootResult.Events[0].Actor, &actorPayload); err != nil || actorPayload["type"] != store.ActorTypeHuman || actorPayload["id"] != author.ID {
		t.Fatalf("actor=%s err=%v", rootResult.Events[0].Actor, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(rootResult.Events[0].Payload, &payload); err != nil || payload["commentId"] != root.ID {
		t.Fatalf("payload=%s err=%v", rootResult.Events[0].Payload, err)
	}

	replyResult, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, ParentCommentID: &root.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "Reply",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replyResult.Comment.ParentCommentID == nil || *replyResult.Comment.ParentCommentID != root.ID {
		t.Fatalf("reply=%+v", replyResult.Comment)
	}

	for name, scope := range map[string][2]string{
		"sibling issue":   {project.ID, sibling.ID},
		"foreign project": {otherProject.ID, foreign.ID},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.CreateIssueComment(ctx, scope[0], store.IssueComment{
				IssueID: scope[1], ParentCommentID: &root.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "invalid parent",
			})
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("error=%v", err)
			}
		})
	}

	reopened := New(pool)
	comments, err := reopened.ListIssueComments(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || comments[0].ID != root.ID || comments[1].ID != replyResult.Comment.ID {
		t.Fatalf("comments after reopen=%+v", comments)
	}
	if comments[0].Body != "Root @nobody stays plain text" {
		t.Fatalf("mention-looking text was not preserved literally: %q", comments[0].Body)
	}

	issueID := issue.ID
	if _, err := s.AppendEvent(ctx, store.Event{
		Type: "tool.completed", ProjectID: project.ID, IssueID: &issueID,
		Actor: store.EmptyObject, Payload: json.RawMessage(`{"name":"ignored-tool"}`),
	}); err != nil {
		t.Fatal(err)
	}
	includedEvent, err := s.AppendEvent(ctx, store.Event{
		Type: "issue.updated", ProjectID: project.ID, IssueID: &issueID,
		Actor: store.EmptyObject, Payload: json.RawMessage(`{"message":"visible activity"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	timelineEvents, err := reopened.ListIssueTimelineEvents(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundIncluded := false
	for _, event := range timelineEvents {
		if event.Type == "tool.completed" || event.Type == "issue.comment_created" {
			t.Fatalf("operational/comment-notification event leaked into Issue timeline: %+v", event)
		}
		if event.ID == includedEvent.ID {
			foundIncluded = true
		}
	}
	if !foundIncluded {
		t.Fatalf("expected Issue activity event %s in timeline: %+v", includedEvent.ID, timelineEvents)
	}

	foreignComments, err := reopened.ListIssueComments(ctx, otherProject.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(foreignComments) != 0 {
		t.Fatalf("cross-project comments leaked: %+v", foreignComments)
	}

	tied := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE issue_id=$2`, tied, issue.ID); err != nil {
		t.Fatal(err)
	}
	comments, err = reopened.ListIssueComments(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{root.ID, replyResult.Comment.ID}
	slices.Sort(ids)
	if len(comments) != 2 || comments[0].ID != ids[0] || comments[1].ID != ids[1] {
		t.Fatalf("tied ordering=%+v want=%v", comments, ids)
	}

	after, err := s.GetIssue(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterRuns, err := s.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var afterExecutionEvents int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM events
		WHERE issue_id=$1 AND type = ANY (ARRAY['run.created','run.cancelled','run.resumed'])
	`, issue.ID).Scan(&afterExecutionEvents); err != nil {
		t.Fatal(err)
	}
	if before.Status != after.Status || before.AssigneeType != nil || after.AssigneeType != nil || len(beforeRuns) != len(afterRuns) ||
		beforeExecutionEvents != afterExecutionEvents {
		t.Fatalf("comment changed workflow: before=%+v after=%+v runs=%d/%d executionEvents=%d/%d", before, after, len(beforeRuns), len(afterRuns), beforeExecutionEvents, afterExecutionEvents)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, author.ID); err != nil {
		t.Fatal(err)
	}
	comments, err = reopened.ListIssueComments(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range comments {
		if comment.AuthorName != "" {
			t.Fatalf("deleted historical author resolved unexpectedly: %+v", comment)
		}
	}
}


func TestIssueCommentLifecyclePersistenceResolutionReactionsAndDeleteRace(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	author, err := s.CreateUser(ctx, authUser("lifecycle-author", "lifecycle-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	reactor, err := s.CreateUser(ctx, authUser("lifecycle-reactor", "lifecycle-reactor@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, testProjectInput("Lifecycle comments", "/repo/lifecycle-comments", "LCM"))
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := s.CreateProject(ctx, testProjectInput("Other lifecycle", "/repo/other-lifecycle", "OLC"))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Lifecycle", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}

	before, err := s.GetIssue(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeRuns, err := s.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}

	rootResult, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "original body",
	})
	if err != nil {
		t.Fatal(err)
	}
	root := rootResult.Comment
	replyResult, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, ParentCommentID: &root.ID, AuthorType: store.ActorTypeHuman, AuthorID: reactor.ID, Body: "reply survives",
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Millisecond)
	edited, err := s.UpdateIssueComment(ctx, project.ID, issue.ID, root.ID, author.ID, "edited body")
	if err != nil {
		t.Fatal(err)
	}
	if edited.Comment.Body != "edited body" || !edited.Comment.UpdatedAt.After(root.UpdatedAt) || len(edited.Events) != 1 {
		t.Fatalf("edited=%+v events=%+v", edited.Comment, edited.Events)
	}
	noOpEdit, err := s.UpdateIssueComment(ctx, project.ID, issue.ID, root.ID, author.ID, "edited body")
	if err != nil || len(noOpEdit.Events) != 0 {
		t.Fatalf("no-op edit events=%+v err=%v", noOpEdit.Events, err)
	}

	resolved, err := s.ResolveIssueComment(ctx, project.ID, issue.ID, root.ID, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Comment.ResolvedAt == nil || resolved.Comment.ResolvedByUserID == nil || *resolved.Comment.ResolvedByUserID != author.ID {
		t.Fatalf("resolved=%+v", resolved.Comment)
	}
	firstResolvedAt := *resolved.Comment.ResolvedAt
	resolvedAgain, err := s.ResolveIssueComment(ctx, project.ID, issue.ID, root.ID, reactor.ID)
	if err != nil || len(resolvedAgain.Events) != 0 || resolvedAgain.Comment.ResolvedByUserID == nil || *resolvedAgain.Comment.ResolvedByUserID != author.ID || !resolvedAgain.Comment.ResolvedAt.Equal(firstResolvedAt) {
		t.Fatalf("idempotent resolve=%+v events=%+v err=%v", resolvedAgain.Comment, resolvedAgain.Events, err)
	}
	if _, err := s.ResolveIssueComment(ctx, project.ID, issue.ID, replyResult.Comment.ID, author.ID); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("reply resolve error=%v", err)
	}

	reopened, err := s.ReopenIssueComment(ctx, project.ID, issue.ID, root.ID, reactor.ID)
	if err != nil || reopened.Comment.ResolvedAt != nil || reopened.Comment.ResolvedByUserID != nil || len(reopened.Events) != 1 {
		t.Fatalf("reopened=%+v events=%+v err=%v", reopened.Comment, reopened.Events, err)
	}
	reopenedAgain, err := s.ReopenIssueComment(ctx, project.ID, issue.ID, root.ID, author.ID)
	if err != nil || len(reopenedAgain.Events) != 0 {
		t.Fatalf("idempotent reopen events=%+v err=%v", reopenedAgain.Events, err)
	}

	events, err := s.AddIssueCommentReaction(ctx, project.ID, issue.ID, root.ID, reactor.ID, store.IssueCommentReactionHeart)
	if err != nil || len(events) != 1 {
		t.Fatalf("add reaction events=%+v err=%v", events, err)
	}
	events, err = s.AddIssueCommentReaction(ctx, project.ID, issue.ID, root.ID, reactor.ID, store.IssueCommentReactionHeart)
	if err != nil || len(events) != 0 {
		t.Fatalf("duplicate reaction events=%+v err=%v", events, err)
	}
	comments, err := s.ListIssueComments(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || len(comments[0].Reactions) != 1 || comments[0].Reactions[0].Reaction != store.IssueCommentReactionHeart ||
		comments[0].Reactions[0].Count != 1 || len(comments[0].Reactions[0].ActorIDs) != 1 || comments[0].Reactions[0].ActorIDs[0] != reactor.ID {
		t.Fatalf("reaction projection=%+v", comments)
	}
	events, err = s.RemoveIssueCommentReaction(ctx, project.ID, issue.ID, root.ID, reactor.ID, store.IssueCommentReactionHeart)
	if err != nil || len(events) != 1 {
		t.Fatalf("remove reaction events=%+v err=%v", events, err)
	}
	events, err = s.RemoveIssueCommentReaction(ctx, project.ID, issue.ID, root.ID, reactor.ID, store.IssueCommentReactionHeart)
	if err != nil || len(events) != 0 {
		t.Fatalf("duplicate remove events=%+v err=%v", events, err)
	}
	if _, err := s.AddIssueCommentReaction(ctx, project.ID, issue.ID, root.ID, reactor.ID, "PARTY"); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid reaction error=%v", err)
	}
	if _, err := s.AddIssueCommentReaction(ctx, otherProject.ID, issue.ID, root.ID, reactor.ID, store.IssueCommentReactionHeart); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project reaction error=%v", err)
	}

	if _, err := s.AddIssueCommentReaction(ctx, project.ID, issue.ID, root.ID, reactor.ID, store.IssueCommentReactionEyes); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.DeleteIssueComment(ctx, project.ID, issue.ID, root.ID, author.ID)
	if err != nil || len(deleted.Events) != 1 {
		t.Fatalf("delete root events=%+v err=%v", deleted.Events, err)
	}
	tombstone, err := s.GetIssueComment(ctx, project.ID, issue.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tombstone.DeletedAt == nil || tombstone.Body != "" || len(tombstone.Reactions) != 0 {
		t.Fatalf("tombstone=%+v", tombstone)
	}
	child, err := s.GetIssueComment(ctx, project.ID, issue.ID, replyResult.Comment.ID)
	if err != nil || child.ParentCommentID == nil || *child.ParentCommentID != root.ID || child.Body != "reply survives" {
		t.Fatalf("child=%+v err=%v", child, err)
	}
	if _, err := s.AddIssueCommentReaction(ctx, project.ID, issue.ID, root.ID, reactor.ID, store.IssueCommentReactionHeart); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("reaction to tombstone error=%v", err)
	}

	leafResult, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "leaf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteIssueComment(ctx, project.ID, issue.ID, leafResult.Comment.ID, author.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetIssueComment(ctx, project.ID, issue.ID, leafResult.Comment.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted leaf still readable err=%v", err)
	}

	raceRootResult, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "race root",
	})
	if err != nil {
		t.Fatal(err)
	}
	raceRoot := raceRootResult.Comment
	start := make(chan struct{})
	replyDone := make(chan error, 1)
	deleteDone := make(chan error, 1)
	go func() {
		<-start
		_, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
			IssueID: issue.ID, ParentCommentID: &raceRoot.ID, AuthorType: store.ActorTypeHuman, AuthorID: reactor.ID, Body: "racing reply",
		})
		replyDone <- err
	}()
	go func() {
		<-start
		_, err := s.DeleteIssueComment(ctx, project.ID, issue.ID, raceRoot.ID, author.ID)
		deleteDone <- err
	}()
	close(start)
	replyErr, deleteErr := <-replyDone, <-deleteDone
	if deleteErr != nil {
		t.Fatalf("racing delete failed: %v", deleteErr)
	}
	raceComments, err := s.ListIssueComments(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	var raceParent *store.IssueComment
	var raceReply *store.IssueComment
	for index := range raceComments {
		comment := &raceComments[index]
		if comment.ID == raceRoot.ID {
			raceParent = comment
		}
		if comment.ParentCommentID != nil && *comment.ParentCommentID == raceRoot.ID {
			raceReply = comment
		}
	}
	if replyErr == nil {
		if raceParent == nil || raceParent.DeletedAt == nil || raceReply == nil {
			t.Fatalf("successful racing reply lost tree parent=%+v reply=%+v", raceParent, raceReply)
		}
	} else {
		if !errors.Is(replyErr, store.ErrNotFound) || raceParent != nil || raceReply != nil {
			t.Fatalf("racing reply error=%v parent=%+v reply=%+v", replyErr, raceParent, raceReply)
		}
	}

	reopenedStore := New(pool)
	reloaded, err := reopenedStore.ListIssueComments(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundTombstone := false
	for _, comment := range reloaded {
		if comment.ID == root.ID {
			foundTombstone = comment.DeletedAt != nil && comment.Body == ""
		}
	}
	if !foundTombstone {
		t.Fatalf("tombstone did not survive reload: %+v", reloaded)
	}

	after, err := s.GetIssue(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterRuns, err := s.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != after.Status || before.AssigneeType != after.AssigneeType || before.AssigneeID != after.AssigneeID || len(beforeRuns) != len(afterRuns) {
		t.Fatalf("comment lifecycle changed workflow before=%+v after=%+v runs=%d/%d", before, after, len(beforeRuns), len(afterRuns))
	}

	timelineEvents, err := s.ListIssueTimelineEvents(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range timelineEvents {
		if event.Type == "issue.comment_created" || event.Type == "issue.comment_changed" {
			t.Fatalf("comment notification leaked into visible timeline: %+v", event)
		}
	}
}
