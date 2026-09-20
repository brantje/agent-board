package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueDiscussionReadsAreThreadAwareBoundedAndCursorSafe(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	author, err := s.CreateUser(ctx, authUser("discussion-reader", "discussion-reader@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, testProjectInput("Discussion reads", "/repo/discussion-reads", "DSC"))
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := s.CreateProject(ctx, testProjectInput("Other discussion reads", "/repo/other-discussion-reads", "ODR"))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Thread aware reads", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}

	create := func(body string, parent *string) store.IssueComment {
		t.Helper()
		result, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
			IssueID: issue.ID, ParentCommentID: parent, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: body,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result.Comment
	}
	root := create("root", nil)
	reply := create("reply", &root.ID)
	grandchild := create("grandchild", &reply.ID)
	secondRoot := create("second root", nil)

	base := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	for index, comment := range []store.IssueComment{root, reply, grandchild, secondRoot} {
		at := base.Add(time.Duration(index) * time.Second)
		if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, at, comment.ID); err != nil {
			t.Fatal(err)
		}
	}

	roots, err := s.ListIssueDiscussionRoots(ctx, project.ID, issue.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 || roots[0].Root.ID != secondRoot.ID || roots[1].Root.ID != root.ID {
		t.Fatalf("roots=%+v", roots)
	}
	if roots[1].ReplyCount != 2 || !roots[1].LastActivityAt.Equal(base.Add(2*time.Second)) || roots[1].Truncated {
		t.Fatalf("thread metadata=%+v", roots[1])
	}

	if _, err := s.ResolveIssueComment(ctx, project.ID, issue.ID, root.ID, author.ID); err != nil {
		t.Fatal(err)
	}
	resolvedRoots, err := s.ListIssueDiscussionRoots(ctx, project.ID, issue.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	var resolvedRoot store.IssueDiscussionRoot
	for _, candidate := range resolvedRoots {
		if candidate.Root.ID == root.ID {
			resolvedRoot = candidate
			break
		}
	}
	if len(resolvedRoot.CompactComments) != 3 {
		t.Fatalf("resolved compact=%+v", resolvedRoot)
	}
	for index, want := range []string{root.ID, reply.ID, grandchild.ID} {
		if resolvedRoot.CompactComments[index].ID != want {
			t.Fatalf("resolved compact ids=%+v", resolvedRoot.CompactComments)
		}
	}

	thread, err := s.GetIssueDiscussionThread(ctx, project.ID, issue.ID, grandchild.ID, 8, 10)
	if err != nil {
		t.Fatal(err)
	}
	if thread.RootID != root.ID || thread.AnchorID != grandchild.ID || thread.Truncated || len(thread.Comments) != 3 {
		t.Fatalf("thread=%+v", thread)
	}
	for index, want := range []string{root.ID, reply.ID, grandchild.ID} {
		if thread.Comments[index].ID != want {
			t.Fatalf("thread ids=%+v", thread.Comments)
		}
	}
	if _, err := s.GetIssueDiscussionThread(ctx, project.ID, issue.ID, grandchild.ID, 8, 2); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("bounded thread insufficient ancestor budget error=%v", err)
	}
	if _, err := s.GetIssueDiscussionThread(ctx, project.ID, issue.ID, grandchild.ID, 1, 10); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("deep anchor error=%v", err)
	}

	lateRoot := create("late root", nil)
	for index := 0; index < 4; index++ {
		create("early sibling", &lateRoot.ID)
	}
	lateParent := create("late parent", &lateRoot.ID)
	lateAnchor := create("late anchor", &lateParent.ID)
	lateThread, err := s.GetIssueDiscussionThread(ctx, project.ID, issue.ID, lateAnchor.ID, 8, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !lateThread.Truncated || len(lateThread.Comments) != 3 {
		t.Fatalf("late bounded thread=%+v", lateThread)
	}
	for index, want := range []string{lateRoot.ID, lateParent.ID, lateAnchor.ID} {
		if lateThread.Comments[index].ID != want {
			t.Fatalf("late bounded ids=%+v", lateThread.Comments)
		}
	}
	if _, err := s.GetIssueDiscussionThread(ctx, project.ID, issue.ID, lateAnchor.ID, 8, 2); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("late anchor insufficient budget error=%v", err)
	}

	cursor := store.IssueCommentCursor{CreatedAt: base.Add(time.Second), ID: reply.ID}
	updates, err := s.ListIssueDiscussionUpdates(ctx, project.ID, issue.ID, &cursor, 1, 16, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !updates.HasMore || updates.NextCursor == nil || updates.NextCursor.ID != grandchild.ID || len(updates.Comments) != 3 {
		t.Fatalf("updates=%+v", updates)
	}
	newCount := 0
	for _, item := range updates.Comments {
		if item.IsNew {
			newCount++
			if item.Comment.ID != grandchild.ID {
				t.Fatalf("unexpected new comment=%+v", item)
			}
		}
	}
	if newCount != 1 {
		t.Fatalf("new count=%d updates=%+v", newCount, updates)
	}

	cursorLeaf := create("cursor leaf", nil)
	cursorAt := base.Add(4 * time.Second)
	if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, cursorAt, cursorLeaf.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteIssueComment(ctx, project.ID, issue.ID, cursorLeaf.ID, author.ID); err != nil {
		t.Fatal(err)
	}
	newer := create("new after deleted cursor", nil)
	newerAt := base.Add(5 * time.Second)
	if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, newerAt, newer.ID); err != nil {
		t.Fatal(err)
	}
	updates, err = s.ListIssueDiscussionUpdates(ctx, project.ID, issue.ID, &store.IssueCommentCursor{CreatedAt: cursorAt, ID: cursorLeaf.ID}, 10, 32, 8)
	if err != nil {
		t.Fatal(err)
	}
	if updates.NextCursor == nil || updates.NextCursor.ID != newer.ID {
		t.Fatalf("deleted cursor updates=%+v", updates)
	}

	foreignRoots, err := s.ListIssueDiscussionRoots(ctx, otherProject.ID, issue.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(foreignRoots) != 0 {
		t.Fatalf("cross-project roots leaked=%+v", foreignRoots)
	}
	if _, err := s.GetIssueDiscussionThread(ctx, otherProject.ID, issue.ID, root.ID, 8, 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project thread error=%v", err)
	}

	otherIssue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Other issue", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetIssueDiscussionThread(ctx, project.ID, otherIssue.ID, root.ID, 8, 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-issue thread error=%v", err)
	}

	tieRoot := create("tie root", nil)
	tieAt := base.Add(10 * time.Second)
	for _, id := range []string{secondRoot.ID, tieRoot.ID} {
		if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, tieAt, id); err != nil {
			t.Fatal(err)
		}
	}
	tiedRoots, err := s.ListIssueDiscussionRoots(ctx, project.ID, issue.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	wantFirst, wantSecond := secondRoot.ID, tieRoot.ID
	if strings.Compare(wantFirst, wantSecond) < 0 {
		wantFirst, wantSecond = wantSecond, wantFirst
	}
	if len(tiedRoots) < 2 || tiedRoots[0].Root.ID != wantFirst || tiedRoots[1].Root.ID != wantSecond {
		t.Fatalf("deterministic tied roots=%+v want first=%s second=%s", tiedRoots, wantFirst, wantSecond)
	}
}
