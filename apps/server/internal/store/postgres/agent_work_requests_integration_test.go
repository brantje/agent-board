package postgres

import (
	"context"
	"testing"
	"time"

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


func TestAgentWorkRequestExecutionContextPreservesFoldedCommentProvenance(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("work-context-author", "work-context-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	root := createPlainWorkRequestComment(t, f, author.ID, "root collaboration context")
	first := createHumanMentionComment(t, f, author.ID, "context-first", "first requested change")
	if len(first.Mentions) != 1 || first.Mentions[0].WorkRequestID == nil || first.Mentions[0].DelegatedRunID == nil {
		t.Fatalf("first=%+v", first)
	}
	parentID := root.ID
	actionKey := "context-second"
	secondResult, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, ParentCommentID: &parentID, AuthorType: store.ActorTypeHuman,
		AuthorID: author.ID, SourceActionKey: &actionKey, Body: "second folded reply",
	}, []string{f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	second := secondResult.Comment
	if len(second.Mentions) != 1 || second.Mentions[0].Outcome != store.IssueCommentMentionOutcomeCoalesced ||
		second.Mentions[0].WorkRequestID == nil || *second.Mentions[0].WorkRequestID != *first.Mentions[0].WorkRequestID {
		t.Fatalf("second=%+v first=%+v", second, first)
	}

	context, err := f.store.GetAgentWorkRequestExecutionContext(ctx, f.project.ID, *first.Mentions[0].DelegatedRunID)
	if err != nil {
		t.Fatal(err)
	}
	if context == nil || context.WorkRequestID != *first.Mentions[0].WorkRequestID || len(context.Comments) != 2 {
		t.Fatalf("context=%+v", context)
	}
	byID := map[string]store.AgentWorkRequestComment{}
	for _, comment := range context.Comments {
		byID[comment.CommentID] = comment
	}
	firstInput, ok := byID[first.ID]
	if !ok || firstInput.Body != first.Body || firstInput.AuthorType != store.ActorTypeHuman ||
		firstInput.AuthorID != author.ID || firstInput.TriggerKind != store.AgentWorkRequestTriggerMention ||
		firstInput.RootCommentID == nil || *firstInput.RootCommentID != first.ID {
		t.Fatalf("first input=%+v", firstInput)
	}
	secondInput, ok := byID[second.ID]
	if !ok || secondInput.Body != second.Body || secondInput.ParentCommentID == nil || *secondInput.ParentCommentID != root.ID ||
		secondInput.RootCommentID == nil || *secondInput.RootCommentID != root.ID ||
		secondInput.TriggerKind != store.AgentWorkRequestTriggerMention {
		t.Fatalf("second input=%+v", secondInput)
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



func TestCommentCoalescingRacingSchedulerAdmissionDoesNotDeadlock(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='COMPLETED', completed_at=now(), updated_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	author, err := f.store.CreateUser(ctx, authUser("work-race-author", "work-race-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	first := createHumanMentionWork(t, f, author.ID, "race-first", "Initial queued work")
	if first.Outcome != store.IssueCommentMentionOutcomeQueued || first.DelegatedRunID == nil {
		t.Fatalf("first=%+v", first)
	}

	runner, err := f.store.CreateRunner(ctx, store.Runner{Name: "comment coalescing race runner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.ObserveRunner(ctx, runner.ID, []byte(`{"max_active_sessions":1}`)); err != nil {
		t.Fatal(err)
	}
	f.store.SetRunnerCandidates(func(string) []string { return []string{runner.ID} })

	raceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	admitResult := make(chan error, 1)
	commentResult := make(chan struct {
		comment store.IssueComment
		err     error
	}, 1)

	go func() {
		<-start
		_, err := f.store.AdmitNextJob(raceCtx, "comment-work-race", time.Minute, time.Millisecond)
		admitResult <- err
	}()
	go func() {
		<-start
		actionKey := "race-second"
		result, err := f.store.CreateIssueCommentWithMentions(raceCtx, f.project.ID, store.IssueComment{
			IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID,
			SourceActionKey: &actionKey, Body: "Concurrent follow-up",
		}, []string{f.target.ID})
		commentResult <- struct {
			comment store.IssueComment
			err     error
		}{comment: result.Comment, err: err}
	}()
	close(start)

	if err := <-admitResult; err != nil {
		t.Fatalf("scheduler admission failed during comment race: %v", err)
	}
	posted := <-commentResult
	if posted.err != nil {
		t.Fatalf("comment failed during scheduler admission race: %v", posted.err)
	}
	if len(posted.comment.Mentions) != 1 {
		t.Fatalf("mentions=%+v", posted.comment.Mentions)
	}
	outcome := posted.comment.Mentions[0].Outcome
	if outcome != store.IssueCommentMentionOutcomeCoalesced && outcome != store.IssueCommentMentionOutcomeDeferred {
		t.Fatalf("race outcome=%s want COALESCED or DEFERRED", outcome)
	}

	runs, err := f.store.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("scheduler/comment race multiplied Runs: %+v", runs)
	}
}


func TestResumedQueuedRunDefersNewCommentWork(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='COMPLETED', completed_at=now(), updated_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	author, err := f.store.CreateUser(ctx, authUser("work-resumed-author", "work-resumed-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	first := createHumanMentionWork(t, f, author.ID, "resumed-first", "Initial queued work")
	if first.Outcome != store.IssueCommentMentionOutcomeQueued || first.WorkRequestID == nil || first.DelegatedRunID == nil {
		t.Fatalf("first=%+v", first)
	}
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE runs
		SET status='QUEUED', started_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, *first.DelegatedRunID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE agent_work_requests
		SET sealed_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, *first.WorkRequestID); err != nil {
		t.Fatal(err)
	}

	followUp := createHumanMentionWork(t, f, author.ID, "resumed-follow-up", "Follow-up after prior admission")
	if followUp.Outcome != store.IssueCommentMentionOutcomeDeferred || followUp.WorkRequestID == nil ||
		*followUp.WorkRequestID == *first.WorkRequestID || followUp.DelegatedRunID != nil {
		t.Fatalf("resumed follow-up=%+v first=%+v", followUp, first)
	}
}

func TestSchedulerAdmissionSealsAgentWorkRequestBoundary(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='COMPLETED', completed_at=now(), updated_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	author, err := f.store.CreateUser(ctx, authUser("work-boundary-author", "work-boundary-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	first := createHumanMentionWork(t, f, author.ID, "boundary-first", "Start this work")
	if first.Outcome != store.IssueCommentMentionOutcomeQueued || first.WorkRequestID == nil || first.DelegatedRunID == nil {
		t.Fatalf("first mention=%+v", first)
	}

	runner, err := f.store.CreateRunner(ctx, store.Runner{Name: "work request boundary runner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.ObserveRunner(ctx, runner.ID, []byte(`{"max_active_sessions":1}`)); err != nil {
		t.Fatal(err)
	}
	f.store.SetRunnerCandidates(func(string) []string { return []string{runner.ID} })
	claim, err := f.store.AdmitNextJob(ctx, "work-request-boundary", time.Minute, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || claim.Run.ID != *first.DelegatedRunID || claim.Run.Status != "STARTING" {
		t.Fatalf("admission=%+v first=%+v", claim, first)
	}

	var sealed bool
	if err := f.store.pool.QueryRow(ctx, `SELECT sealed_at IS NOT NULL FROM agent_work_requests WHERE project_id=$1 AND id=$2`, f.project.ID, *first.WorkRequestID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if !sealed {
		t.Fatal("admitted Run left its comment work request open for coalescing")
	}

	followUp := createHumanMentionWork(t, f, author.ID, "boundary-follow-up", "Follow up after admission")
	if followUp.Outcome != store.IssueCommentMentionOutcomeDeferred || followUp.WorkRequestID == nil || *followUp.WorkRequestID == *first.WorkRequestID || followUp.DelegatedRunID != nil {
		t.Fatalf("follow-up=%+v first=%+v", followUp, first)
	}
}

func TestDeferredAgentWorkRequestReconcilesAfterRestartWithoutReplay(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("work-restart-author", "work-restart-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	first := createHumanMentionWork(t, f, author.ID, "restart-first", "Initial work")
	if first.DelegatedRunID == nil || first.WorkRequestID == nil {
		t.Fatalf("first mention=%+v", first)
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='RUNNING', started_at=now(), updated_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, *first.DelegatedRunID); err != nil {
		t.Fatal(err)
	}
	deferredComment := createHumanMentionComment(t, f, author.ID, "restart-deferred", "Deferred after current execution")
	deferred := deferredComment.Mentions[0]
	if deferred.Outcome != store.IssueCommentMentionOutcomeDeferred || deferred.WorkRequestID == nil {
		t.Fatalf("deferred mention=%+v", deferred)
	}
	foldedComment := createHumanMentionComment(t, f, author.ID, "restart-folded", "Folded into deferred follow-up")
	folded := foldedComment.Mentions[0]
	if folded.Outcome != store.IssueCommentMentionOutcomeCoalesced || folded.WorkRequestID == nil || *folded.WorkRequestID != *deferred.WorkRequestID {
		t.Fatalf("folded=%+v deferred=%+v", folded, deferred)
	}

	completeRunAndFinalizeWorkRequest(t, f, *first.DelegatedRunID)

	restarted := New(f.store.pool)
	result, err := restarted.ReconcilePendingAgentWorkRequest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Handled || len(result.Events) != 1 || result.Events[0].Type != "run.created" {
		t.Fatalf("reconciliation=%+v", result)
	}

	reloadedDeferred, err := restarted.GetIssueComment(ctx, f.project.ID, f.issue.ID, deferredComment.ID)
	if err != nil {
		t.Fatal(err)
	}
	reloadedFolded, err := restarted.GetIssueComment(ctx, f.project.ID, f.issue.ID, foldedComment.ID)
	if err != nil {
		t.Fatal(err)
	}
	left, right := reloadedDeferred.Mentions[0], reloadedFolded.Mentions[0]
	if left.WorkRequestID == nil || right.WorkRequestID == nil || *left.WorkRequestID != *right.WorkRequestID ||
		left.DelegationID == nil || right.DelegationID == nil || *left.DelegationID != *right.DelegationID ||
		left.DelegatedRunID == nil || right.DelegatedRunID == nil || *left.DelegatedRunID != *right.DelegatedRunID {
		t.Fatalf("promoted provenance deferred=%+v folded=%+v", left, right)
	}
	if left.Outcome != store.IssueCommentMentionOutcomeDeferred || right.Outcome != store.IssueCommentMentionOutcomeCoalesced {
		t.Fatalf("promotion rewrote per-trigger outcomes: deferred=%+v folded=%+v", left, right)
	}

	executionContext, err := restarted.GetAgentWorkRequestExecutionContext(ctx, f.project.ID, *left.DelegatedRunID)
	if err != nil {
		t.Fatal(err)
	}
	if executionContext == nil || executionContext.WorkRequestID != *left.WorkRequestID || len(executionContext.Comments) != 2 {
		t.Fatalf("promoted execution context=%+v", executionContext)
	}
	commentIDs := map[string]bool{}
	for _, comment := range executionContext.Comments {
		commentIDs[comment.CommentID] = true
	}
	if !commentIDs[deferredComment.ID] || !commentIDs[foldedComment.ID] {
		t.Fatalf("promoted execution context lost folded provenance: %+v", executionContext.Comments)
	}

	completeRunAndFinalizeWorkRequest(t, f, *left.DelegatedRunID)
	again, err := restarted.ReconcilePendingAgentWorkRequest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.Handled {
		t.Fatalf("completed deferred request replayed: %+v", again)
	}
	runs, err := restarted.ListRuns(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("restart reconciliation multiplied Runs: %+v", runs)
	}
}


func TestFailedActiveCommentRunReleasesDeferredFollowUp(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("work-failure-author", "work-failure-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	first := createHumanMentionWork(t, f, author.ID, "failure-first", "Initial work that fails")
	if first.DelegatedRunID == nil || first.WorkRequestID == nil {
		t.Fatalf("first=%+v", first)
	}
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE runs
		SET status='RUNNING', started_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, *first.DelegatedRunID); err != nil {
		t.Fatal(err)
	}
	deferred := createHumanMentionWork(t, f, author.ID, "failure-deferred", "Follow up after failure")
	if deferred.Outcome != store.IssueCommentMentionOutcomeDeferred || deferred.WorkRequestID == nil || deferred.DelegatedRunID != nil {
		t.Fatalf("deferred=%+v", deferred)
	}

	tx, err := f.store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	failedRun, err := scanRun(tx.QueryRow(ctx, `
		UPDATE runs
		SET status='FAILED', failure_reason='test failure', completed_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		          status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, f.project.ID, *first.DelegatedRunID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalizeDelegatedRunTx(ctx, tx, failedRun); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	reconciled, err := f.store.ReconcilePendingAgentWorkRequest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reconciled.Handled || len(reconciled.Events) != 1 || reconciled.Events[0].Type != "run.created" {
		t.Fatalf("failed active work stranded deferred follow-up: %+v", reconciled)
	}
	reloaded, err := f.store.GetIssueComment(ctx, f.project.ID, f.issue.ID, findMentionCommentIDByWorkRequest(t, f, *deferred.WorkRequestID))
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Mentions) != 1 || reloaded.Mentions[0].DelegatedRunID == nil {
		t.Fatalf("deferred follow-up was not promoted after failure: %+v", reloaded.Mentions)
	}
}

func TestDeferredAgentWorkRequestClosesWhenTriggerCommentIsDeleted(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("work-deleted-author", "work-deleted-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	first := createHumanMentionWork(t, f, author.ID, "deleted-first", "Initial active work")
	if first.DelegatedRunID == nil {
		t.Fatalf("first=%+v", first)
	}
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE runs
		SET status='RUNNING', started_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, *first.DelegatedRunID); err != nil {
		t.Fatal(err)
	}
	deferredComment := createHumanMentionComment(t, f, author.ID, "deleted-deferred", "Follow-up that is withdrawn")
	deferred := deferredComment.Mentions[0]
	if deferred.Outcome != store.IssueCommentMentionOutcomeDeferred || deferred.WorkRequestID == nil {
		t.Fatalf("deferred=%+v", deferred)
	}

	completeRunAndFinalizeWorkRequest(t, f, *first.DelegatedRunID)
	if _, err := f.store.DeleteIssueComment(ctx, f.project.ID, f.issue.ID, deferredComment.ID, author.ID); err != nil {
		t.Fatal(err)
	}

	reconciled, err := f.store.ReconcilePendingAgentWorkRequest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reconciled.Handled || len(reconciled.Events) != 0 {
		t.Fatalf("deleted trigger reconciliation=%+v", reconciled)
	}

	var sealed, closed bool
	var runID, delegationID *string
	if err := f.store.pool.QueryRow(ctx, `
		SELECT sealed_at IS NOT NULL, closed_at IS NOT NULL, run_id::text, delegation_id::text
		FROM agent_work_requests
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, *deferred.WorkRequestID).Scan(&sealed, &closed, &runID, &delegationID); err != nil {
		t.Fatal(err)
	}
	if !sealed || !closed || runID != nil || delegationID != nil {
		t.Fatalf("withdrawn work request sealed=%v closed=%v run=%v delegation=%v", sealed, closed, runID, delegationID)
	}
}

func TestDeferredAgentWorkRequestWaitsForTemporarilyUnavailableTarget(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("work-unavailable-author", "work-unavailable-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	first := createHumanMentionWork(t, f, author.ID, "unavailable-first", "Initial active work")
	if first.DelegatedRunID == nil {
		t.Fatalf("first=%+v", first)
	}
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE runs
		SET status='RUNNING', started_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, *first.DelegatedRunID); err != nil {
		t.Fatal(err)
	}
	deferred := createHumanMentionWork(t, f, author.ID, "unavailable-deferred", "Follow-up after the target is available again")
	if deferred.Outcome != store.IssueCommentMentionOutcomeDeferred || deferred.WorkRequestID == nil {
		t.Fatalf("deferred=%+v", deferred)
	}
	completeRunAndFinalizeWorkRequest(t, f, *first.DelegatedRunID)

	if _, err := f.store.pool.Exec(ctx, `UPDATE agents SET state='DISABLED' WHERE id=$1`, f.target.ID); err != nil {
		t.Fatal(err)
	}
	blocked, err := f.store.ReconcilePendingAgentWorkRequest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Handled || len(blocked.Events) != 0 {
		t.Fatalf("temporarily unavailable target should leave work pending: %+v", blocked)
	}
	var open bool
	if err := f.store.pool.QueryRow(ctx, `
		SELECT sealed_at IS NULL AND closed_at IS NULL AND run_id IS NULL AND delegation_id IS NULL
		FROM agent_work_requests
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, *deferred.WorkRequestID).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if !open {
		t.Fatal("temporarily unavailable target terminalized deferred work")
	}

	if _, err := f.store.pool.Exec(ctx, `UPDATE agents SET state='ENABLED' WHERE id=$1`, f.target.ID); err != nil {
		t.Fatal(err)
	}
	reconciled, err := f.store.ReconcilePendingAgentWorkRequest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reconciled.Handled || len(reconciled.Events) != 1 || reconciled.Events[0].Type != "run.created" {
		t.Fatalf("restored target did not receive deferred work: %+v", reconciled)
	}
}

func findMentionCommentIDByWorkRequest(t *testing.T, f delegationFixture, workRequestID string) string {
	t.Helper()
	var commentID string
	if err := f.store.pool.QueryRow(t.Context(), `
		SELECT comment_id::text
		FROM issue_comment_mentions
		WHERE project_id=$1 AND work_request_id=$2
		ORDER BY created_at, id
		LIMIT 1
	`, f.project.ID, workRequestID).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	return commentID
}

func TestCancellingQueuedCommentRunClosesRequestAndAllowsFreshWork(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("work-cancel-author", "work-cancel-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	first := createHumanMentionWork(t, f, author.ID, "cancel-first", "Work that will be cancelled")
	if first.DelegatedRunID == nil || first.WorkRequestID == nil {
		t.Fatalf("first=%+v", first)
	}
	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, *first.DelegatedRunID); err != nil {
		t.Fatal(err)
	}
	var sealed, closed bool
	if err := f.store.pool.QueryRow(ctx, `SELECT sealed_at IS NOT NULL, closed_at IS NOT NULL FROM agent_work_requests WHERE project_id=$1 AND id=$2`, f.project.ID, *first.WorkRequestID).Scan(&sealed, &closed); err != nil {
		t.Fatal(err)
	}
	if !sealed || !closed {
		t.Fatalf("cancelled work request sealed=%v closed=%v", sealed, closed)
	}

	second := createHumanMentionWork(t, f, author.ID, "cancel-second", "Fresh work after cancellation")
	if second.Outcome != store.IssueCommentMentionOutcomeQueued || second.WorkRequestID == nil || *second.WorkRequestID == *first.WorkRequestID || second.DelegatedRunID == nil {
		t.Fatalf("fresh work=%+v first=%+v", second, first)
	}
}

func TestAgentCommentDoesNotDeferAcrossIncompatibleParentAuthority(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	author, err := f.store.CreateUser(ctx, authUser("work-parent-author", "work-parent-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	first := createHumanMentionWork(t, f, author.ID, "parent-busy-first", "Human work already queued")
	if first.Outcome != store.IssueCommentMentionOutcomeQueued {
		t.Fatalf("first=%+v", first)
	}

	actionKey := "parent-agent-comment"
	sourceRunID := f.parentRun.ID
	result, err := f.store.CreateIssueCommentWithMentions(ctx, f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeAgent, AuthorID: f.parent.ID,
		SourceRunID: &sourceRunID, SourceActionKey: &actionKey, Body: "Parent-authority request while incompatible work is queued",
	}, []string{f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Comment.Mentions) != 1 {
		t.Fatalf("mentions=%+v", result.Comment.Mentions)
	}
	mention := result.Comment.Mentions[0]
	if mention.Outcome != store.IssueCommentMentionOutcomeBlocked || mention.WorkRequestID != nil || mention.ReasonCode == nil || *mention.ReasonCode != store.IssueCommentMentionReasonDelegationBlocked {
		t.Fatalf("parent-authority mention=%+v", mention)
	}
}

func createHumanMentionWork(t *testing.T, f delegationFixture, authorID, actionKey, body string) store.IssueCommentMention {
	t.Helper()
	comment := createHumanMentionComment(t, f, authorID, actionKey, body)
	if len(comment.Mentions) != 1 {
		t.Fatalf("mentions=%+v", comment.Mentions)
	}
	return comment.Mentions[0]
}

func createHumanMentionComment(t *testing.T, f delegationFixture, authorID, actionKey, body string) store.IssueComment {
	t.Helper()
	result, err := f.store.CreateIssueCommentWithMentions(t.Context(), f.project.ID, store.IssueComment{
		IssueID: f.issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: authorID, SourceActionKey: &actionKey, Body: body,
	}, []string{f.target.ID})
	if err != nil {
		t.Fatal(err)
	}
	return result.Comment
}

func completeRunAndFinalizeWorkRequest(t *testing.T, f delegationFixture, runID string) {
	t.Helper()
	ctx := t.Context()
	tx, err := f.store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err := scanRun(tx.QueryRow(ctx, `
		UPDATE runs
		SET status='COMPLETED', completed_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		          status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, f.project.ID, runID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalizeDelegatedRunTx(ctx, tx, run); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
