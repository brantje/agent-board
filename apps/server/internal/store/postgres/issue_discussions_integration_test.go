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

	emptyComments, err := s.listIssueCommentsByIDs(ctx, project.ID, issue.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emptyComments) != 0 {
		t.Fatalf("empty comment id read=%+v", emptyComments)
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
	lateComments := []store.IssueComment{lateRoot}
	for index := 0; index < 4; index++ {
		lateComments = append(lateComments, create("early sibling", &lateRoot.ID))
	}
	lateParent := create("late parent", &lateRoot.ID)
	lateAnchor := create("late anchor", &lateParent.ID)
	lateComments = append(lateComments, lateParent, lateAnchor)
	for index, comment := range lateComments {
		at := base.Add(3*time.Second + time.Duration(index+1)*10*time.Millisecond)
		if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, at, comment.ID); err != nil {
			t.Fatal(err)
		}
	}
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

func TestIssueDiscussionReadsRejectInvalidBoundsAndAllowEmptyProjection(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()

	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{
			name: "roots missing project",
			run: func() error {
				_, err := s.ListIssueDiscussionRoots(ctx, "", "11111111-1111-4111-8111-111111111111", 1)
				return err
			},
		},
		{
			name: "roots zero limit",
			run: func() error {
				_, err := s.ListIssueDiscussionRoots(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", 0)
				return err
			},
		},
		{
			name: "thread missing anchor",
			run: func() error {
				_, err := s.GetIssueDiscussionThread(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "", 8, 10)
				return err
			},
		},
		{
			name: "thread zero depth",
			run: func() error {
				_, err := s.GetIssueDiscussionThread(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333", 0, 10)
				return err
			},
		},
		{
			name: "thread zero comments",
			run: func() error {
				_, err := s.GetIssueDiscussionThread(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333", 8, 0)
				return err
			},
		},
		{
			name: "updates zero new limit",
			run: func() error {
				_, err := s.ListIssueDiscussionUpdates(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", nil, 0, 200, 64)
				return err
			},
		},
		{
			name: "updates zero depth",
			run: func() error {
				_, err := s.ListIssueDiscussionUpdates(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", nil, 1, 200, 0)
				return err
			},
		},
		{
			name: "updates insufficient context budget",
			run: func() error {
				_, err := s.ListIssueDiscussionUpdates(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", nil, 1, 64, 64)
				return err
			},
		},
		{
			name: "updates invalid cursor",
			run: func() error {
				_, err := s.ListIssueDiscussionUpdates(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", &store.IssueCommentCursor{ID: "33333333-3333-4333-8333-333333333333"}, 1, 200, 64)
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, store.ErrInvalidArgument) {
				t.Fatalf("error=%v", err)
			}
		})
	}

	values, err := s.listIssueCommentsByIDs(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("empty projection=%+v", values)
	}
}

func TestIssueDiscussionReadsBoundWideAndDeepGraphs(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	author, err := s.CreateUser(ctx, authUser("discussion-bounds", "discussion-bounds@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, testProjectInput("Discussion bounds", "/repo/discussion-bounds", "DBD"))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Bound discussion graph work", Status: "TODO"})
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

	base := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	wideRoot := create("wide root", nil)
	if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, base, wideRoot.ID); err != nil {
		t.Fatal(err)
	}
	const wideReplies = 300
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO issue_comments (issue_id, parent_comment_id, author_type, author_id, body, created_at, updated_at)
		SELECT $1, $2, 'HUMAN', $3, 'wide reply ' || n::text,
		       $4 + n * interval '1 microsecond',
		       $4 + n * interval '1 microsecond'
		FROM generate_series(1, $5) AS n
	`, issue.ID, wideRoot.ID, author.ID, base, wideReplies); err != nil {
		t.Fatal(err)
	}

	ids, truncated, err := s.boundedIssueCommentTreeIDs(ctx, project.ID, issue.ID, wideRoot.ID, 8, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 5 || !truncated {
		t.Fatalf("bounded wide tree ids=%d truncated=%v", len(ids), truncated)
	}
	thread, err := s.GetIssueDiscussionThread(ctx, project.ID, issue.ID, wideRoot.ID, 8, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Comments) != 5 || !thread.Truncated {
		t.Fatalf("bounded wide thread=%+v", thread)
	}
	roots, err := s.ListIssueDiscussionRoots(ctx, project.ID, issue.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Root.ID != wideRoot.ID {
		t.Fatalf("wide roots=%+v", roots)
	}
	wantWideActivity := base.Add(wideReplies * time.Microsecond)
	if !roots[0].LastActivityAt.Equal(wantWideActivity) {
		t.Fatalf("wide last activity=%s want=%s", roots[0].LastActivityAt, wantWideActivity)
	}
	if !roots[0].Truncated || roots[0].ReplyCount <= 0 || roots[0].ReplyCount >= wideReplies {
		t.Fatalf("wide metadata=%+v", roots[0])
	}

	deepBase := base.Add(10 * time.Second)
	deepRoot := create("deep root", nil)
	if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, deepBase, deepRoot.ID); err != nil {
		t.Fatal(err)
	}
	parent := deepRoot
	var deepLeaf store.IssueComment
	for index := 0; index < issueDiscussionRootTraversalDepth+1; index++ {
		child := create("deep reply", &parent.ID)
		at := deepBase.Add(time.Duration(index+1) * time.Millisecond)
		if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, at, child.ID); err != nil {
			t.Fatal(err)
		}
		parent = child
		deepLeaf = child
	}
	deepIDs, deepTruncated, err := s.boundedIssueCommentTreeIDs(
		ctx, project.ID, issue.ID, deepRoot.ID, issueDiscussionRootTraversalDepth, issueDiscussionRootMetadataCommentLimit,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !deepTruncated || len(deepIDs) != issueDiscussionRootTraversalDepth+1 {
		t.Fatalf("deep bounded ids=%d truncated=%v", len(deepIDs), deepTruncated)
	}
	if _, err := s.GetIssueDiscussionThread(ctx, project.ID, issue.ID, deepLeaf.ID, issueDiscussionRootTraversalDepth, 100); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("deep thread error=%v", err)
	}

	olderRoot := create("older safe root", nil)
	safeRoot := create("newest safe root", nil)
	if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, deepBase.Add(-time.Second), olderRoot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, deepBase.Add(2*time.Second), safeRoot.ID); err != nil {
		t.Fatal(err)
	}
	recent, err := s.ListIssueDiscussionRoots(ctx, project.ID, issue.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].Root.ID != safeRoot.ID {
		t.Fatalf("recent roots crossed unresolved deep activity boundary: %+v", recent)
	}
}

func TestIssueDiscussionReadsRemainAncestorClosedAcrossTimestampTies(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	author, err := s.CreateUser(ctx, authUser("discussion-ties", "discussion-ties@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, testProjectInput("Discussion ties", "/repo/discussion-ties", "DTI"))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Keep ancestor context under timestamp ties", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	tieAt := time.Date(2026, 9, 20, 17, 0, 0, 0, time.UTC)
	rootID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	parentID := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	childID := "00000000-0000-4000-8000-000000000001"
	for _, input := range []struct {
		id     string
		parent *string
		body   string
	}{
		{id: rootID, body: "root"},
		{id: parentID, parent: &rootID, body: "parent"},
		{id: childID, parent: &parentID, body: "child"},
	} {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO issue_comments (id, issue_id, parent_comment_id, author_type, author_id, body, created_at, updated_at)
			VALUES ($1, $2, $3, 'HUMAN', $4, $5, $6, $6)
		`, input.id, issue.ID, input.parent, author.ID, input.body, tieAt); err != nil {
			t.Fatal(err)
		}
	}

	thread, err := s.GetIssueDiscussionThread(ctx, project.ID, issue.ID, rootID, 8, 3)
	if err != nil {
		t.Fatal(err)
	}
	if thread.Truncated || len(thread.Comments) != 3 {
		t.Fatalf("thread=%+v", thread)
	}
	for index, want := range []string{rootID, parentID, childID} {
		if thread.Comments[index].ID != want {
			t.Fatalf("ancestor order ids=%+v", []string{thread.Comments[0].ID, thread.Comments[1].ID, thread.Comments[2].ID})
		}
	}
	small, err := s.GetIssueDiscussionThread(ctx, project.ID, issue.ID, rootID, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !small.Truncated || len(small.Comments) != 2 || small.Comments[0].ID != rootID || small.Comments[1].ID != parentID {
		t.Fatalf("small ancestor-closed thread=%+v", small)
	}

	cursorResult, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "cursor",
	})
	if err != nil {
		t.Fatal(err)
	}
	cursorAt := tieAt.Add(-time.Second)
	if _, err := s.pool.Exec(ctx, `UPDATE issue_comments SET created_at=$1, updated_at=$1 WHERE id=$2`, cursorAt, cursorResult.Comment.ID); err != nil {
		t.Fatal(err)
	}
	updates, err := s.ListIssueDiscussionUpdates(
		ctx,
		project.ID,
		issue.ID,
		&store.IssueCommentCursor{CreatedAt: cursorAt, ID: cursorResult.Comment.ID},
		1,
		3,
		8,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates.Comments) != 3 || !updates.HasMore || updates.NextCursor == nil || updates.NextCursor.ID != childID {
		t.Fatalf("tied updates=%+v", updates)
	}
	for index, want := range []string{rootID, parentID, childID} {
		if updates.Comments[index].Comment.ID != want {
			t.Fatalf("tied update order=%+v", updates.Comments)
		}
	}
	if updates.Comments[0].IsNew || updates.Comments[1].IsNew || !updates.Comments[2].IsNew {
		t.Fatalf("tied update new markers=%+v", updates.Comments)
	}
	if len(updates.Comments) > 3 {
		t.Fatalf("updates exceeded context budget: %d", len(updates.Comments))
	}
}

