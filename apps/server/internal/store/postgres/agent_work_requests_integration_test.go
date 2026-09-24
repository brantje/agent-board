package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestAgentWorkRequestOpenCompatibilityIsUniqueAndSealable(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	workspaceID := f.parentRun.WorkspaceID

	var issueRequestID string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind)
		VALUES ($1,$2,$3,$4,'ISSUE')
		RETURNING id::text
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID).Scan(&issueRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind)
		VALUES ($1,$2,$3,$4,'ISSUE')
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID); err == nil {
		t.Fatal("duplicate open Issue-authority request unexpectedly succeeded")
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE agent_work_requests SET sealed_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, issueRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind)
		VALUES ($1,$2,$3,$4,'ISSUE')
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID); err != nil {
		t.Fatalf("sealed request should allow a follow-up request: %v", err)
	}

	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind, parent_run_id)
		VALUES ($1,$2,$3,$4,'PARENT_RUN',$5)
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind, parent_run_id)
		VALUES ($1,$2,$3,$4,'PARENT_RUN',$5)
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID, f.parentRun.ID); err == nil {
		t.Fatal("duplicate open parent-authority request unexpectedly succeeded")
	}
}


func TestRequestAgentWorkTxQueuesAndCoalescesCompatibleQueuedWork(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("work-request-author", "work-request-author@example.com", "active"))
	if err != nil {
		t.Fatal(err)
	}
	firstComment := createPlainWorkRequestComment(t, f, author.ID, "first requested change")
	first := requestAgentWorkForTest(t, f, firstComment, nil)
	if first.Outcome != agentWorkRequestOutcomeQueued || first.WorkRequest.RunID == nil || first.WorkRequest.DelegationID == nil {
		t.Fatalf("first request=%+v", first)
	}

	secondComment := createPlainWorkRequestComment(t, f, author.ID, "second compatible request")
	second := requestAgentWorkForTest(t, f, secondComment, nil)
	if second.Outcome != agentWorkRequestOutcomeCoalesced || second.WorkRequest.ID != first.WorkRequest.ID {
		t.Fatalf("coalesced request=%+v first=%+v", second, first)
	}
	if second.WorkRequest.RunID == nil || *second.WorkRequest.RunID != *first.WorkRequest.RunID {
		t.Fatalf("coalesced request changed Run: first=%+v second=%+v", first.WorkRequest, second.WorkRequest)
	}
	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("Runs=%+v want parent plus one target", runs)
	}
}

func TestRequestAgentWorkTxDefersAfterQueuedRunCrossesSafeBoundary(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("work-defer-author", "work-defer-author@example.com", "active"))
	if err != nil {
		t.Fatal(err)
	}
	firstComment := createPlainWorkRequestComment(t, f, author.ID, "initial queued request")
	first := requestAgentWorkForTest(t, f, firstComment, nil)
	if first.WorkRequest.RunID == nil {
		t.Fatalf("queued request has no Run: %+v", first)
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='RUNNING', started_at=now(), updated_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, *first.WorkRequest.RunID); err != nil {
		t.Fatal(err)
	}

	deferredComment := createPlainWorkRequestComment(t, f, author.ID, "follow up while executing")
	deferred := requestAgentWorkForTest(t, f, deferredComment, nil)
	if deferred.Outcome != agentWorkRequestOutcomeDeferred || deferred.WorkRequest.ID == first.WorkRequest.ID || deferred.WorkRequest.RunID != nil || deferred.WorkRequest.DelegationID != nil {
		t.Fatalf("deferred request=%+v first=%+v", deferred, first)
	}

	coalescedComment := createPlainWorkRequestComment(t, f, author.ID, "another follow up")
	coalesced := requestAgentWorkForTest(t, f, coalescedComment, nil)
	if coalesced.Outcome != agentWorkRequestOutcomeCoalesced || coalesced.WorkRequest.ID != deferred.WorkRequest.ID {
		t.Fatalf("deferred coalescing=%+v deferred=%+v", coalesced, deferred)
	}
}

func createPlainWorkRequestComment(t *testing.T, f delegationFixture, authorID, body string) store.IssueComment {
	t.Helper()
	result, err := f.store.CreateIssueComment(t.Context(), f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: authorID, Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.Comment
}

func requestAgentWorkForTest(t *testing.T, f delegationFixture, comment store.IssueComment, parentRunID *string) agentWorkRequestResult {
	t.Helper()
	tx, err := f.store.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	authority := store.AgentWorkRequestAuthorityIssue
	if parentRunID != nil {
		authority = store.AgentWorkRequestAuthorityParentRun
	}
	result, err := f.store.requestAgentWorkTx(t.Context(), tx, agentWorkRequestInput{
		ProjectID: f.project.ID, IssueID: f.issue.ID, SourceCommentID: comment.ID,
		TargetAgentID: f.target.ID, Task: comment.Body, AuthorityKind: authority, ParentRunID: parentRunID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	return result
}
