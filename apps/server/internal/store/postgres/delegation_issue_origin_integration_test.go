package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRequestIssueDelegationCreatesOrdinaryUnheldDelegatedRunAndIsIdempotent(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()

	var commentID string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO issue_comments (issue_id, author_type, author_id, body)
		VALUES ($1, 'HUMAN', gen_random_uuid(), 'Please inspect the focused request.')
		RETURNING id::text
	`, f.issue.ID).Scan(&commentID); err != nil {
		t.Fatal(err)
	}

	input := store.RequestIssueDelegationCommand{
		ProjectID: f.project.ID, IssueID: f.issue.ID, SourceCommentID: commentID,
		TargetAgentID: f.target.ID, Task: "Please inspect the focused request.", RequestKey: "mention-1",
	}
	first, err := f.store.RequestIssueDelegation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Delegation.ParentRunID != "" || first.Delegation.ParentAgentID != "" || first.Delegation.SourceCommentID == nil || *first.Delegation.SourceCommentID != commentID {
		t.Fatalf("unexpected comment-origin lineage: %+v", first.Delegation)
	}
	if first.DelegatedRun.Status != "QUEUED" || first.DelegatedRun.AgentID == nil || *first.DelegatedRun.AgentID != f.target.ID {
		t.Fatalf("unexpected delegated Run: %+v", first.DelegatedRun)
	}
	if first.DelegatedRun.QueueReason != nil || first.SchedulerJob.WaitReason != nil {
		t.Fatalf("comment-origin delegation was incorrectly held for parent handoff: run=%+v job=%+v", first.DelegatedRun, first.SchedulerJob)
	}

	retry, err := f.store.RequestIssueDelegation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Delegation.ID != first.Delegation.ID || retry.DelegatedRun.ID != first.DelegatedRun.ID || retry.SchedulerJob.ID != first.SchedulerJob.ID {
		t.Fatalf("idempotent retry created duplicate work: first=%+v retry=%+v", first, retry)
	}

	changed := input
	changed.TargetAgentID = f.parent.ID
	if _, err := f.store.RequestIssueDelegation(ctx, changed); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("changed retry error=%v want conflict", err)
	}
}

func TestRequestIssueDelegationRejectsCrossIssueCommentAndActiveTarget(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()

	other, err := f.store.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "Other issue", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	var otherCommentID string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO issue_comments (issue_id, author_type, author_id, body)
		VALUES ($1, 'HUMAN', gen_random_uuid(), 'other')
		RETURNING id::text
	`, other.ID).Scan(&otherCommentID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.RequestIssueDelegation(ctx, store.RequestIssueDelegationCommand{
		ProjectID: f.project.ID, IssueID: f.issue.ID, SourceCommentID: otherCommentID,
		TargetAgentID: f.target.ID, Task: "cross issue", RequestKey: "cross-issue",
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-Issue comment error=%v want not found", err)
	}

	var commentID string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO issue_comments (issue_id, author_type, author_id, body)
		VALUES ($1, 'HUMAN', gen_random_uuid(), 'target once')
		RETURNING id::text
	`, f.issue.ID).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	first, err := f.store.RequestIssueDelegation(ctx, store.RequestIssueDelegationCommand{
		ProjectID: f.project.ID, IssueID: f.issue.ID, SourceCommentID: commentID,
		TargetAgentID: f.target.ID, Task: "target once", RequestKey: "active-target-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var secondCommentID string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO issue_comments (issue_id, author_type, author_id, body)
		VALUES ($1, 'HUMAN', gen_random_uuid(), 'target twice')
		RETURNING id::text
	`, f.issue.ID).Scan(&secondCommentID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.RequestIssueDelegation(ctx, store.RequestIssueDelegationCommand{
		ProjectID: f.project.ID, IssueID: f.issue.ID, SourceCommentID: secondCommentID,
		TargetAgentID: f.target.ID, Task: "target twice", RequestKey: "active-target-2",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("active target error=%v want conflict (first=%s)", err, first.DelegatedRun.ID)
	}
}

func TestIssueOriginDelegationTerminalizesWithoutParentContinuation(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()

	var commentID string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO issue_comments (issue_id, author_type, author_id, body)
		VALUES ($1, 'HUMAN', gen_random_uuid(), 'complete independently')
		RETURNING id::text
	`, f.issue.ID).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	created, err := f.store.RequestIssueDelegation(ctx, store.RequestIssueDelegationCommand{
		ProjectID: f.project.ID, IssueID: f.issue.ID, SourceCommentID: commentID,
		TargetAgentID: f.target.ID, Task: "complete independently", RequestKey: "mention-terminal",
	})
	if err != nil {
		t.Fatal(err)
	}

	tx, err := f.store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	child, err := scanRun(tx.QueryRow(ctx, `
		UPDATE runs
		SET status='COMPLETED', completed_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		          status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, f.project.ID, created.DelegatedRun.ID))
	if err != nil {
		t.Fatal(err)
	}
	events, err := finalizeDelegatedRunTx(ctx, tx, child)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].RunID == nil || *events[0].RunID != child.ID || events[0].Type != "delegation.completed" {
		t.Fatalf("terminal events=%+v", events)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeSucceeded || delegation.CompletedAt == nil || delegation.ContinuationJobID != nil {
		t.Fatalf("terminal delegation=%+v", delegation)
	}
}

func TestRequestIssueDelegationRejectsAgentAuthoredSourceComment(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	runID := f.parentRun.ID
	actionKey := "agent-source-comment"
	commentResult, err := f.store.CreateIssueComment(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeAgent, AuthorID: f.parent.ID,
		SourceRunID: &runID, SourceActionKey: &actionKey, Body: "Agent-authored source must retain parent-Run policy.",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.RequestIssueDelegation(ctx, store.RequestIssueDelegationCommand{
		ProjectID: f.project.ID, IssueID: f.issue.ID, SourceCommentID: commentResult.Comment.ID,
		TargetAgentID: f.target.ID, Task: "attempt parentless bypass", RequestKey: "agent-source-bypass",
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Agent-authored source error=%v want not found", err)
	}
	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("Agent-authored parentless request created delegated execution: %+v", runs)
	}
}
