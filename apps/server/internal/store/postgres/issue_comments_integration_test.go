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
	if before.Status != after.Status || before.AssigneeType != nil || after.AssigneeType != nil || len(beforeRuns) != len(afterRuns) {
		t.Fatalf("comment changed workflow: before=%+v after=%+v runs=%d/%d", before, after, len(beforeRuns), len(afterRuns))
	}
}
