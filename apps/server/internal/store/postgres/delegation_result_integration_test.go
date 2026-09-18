package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestDelegatedCompletionPersistsResultAndParentContinuationAtomically(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "result-success")
	ctx := t.Context()
	issueBefore, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}

	workspaceID, childRunID, targetAgentID := created.DelegatedRun.WorkspaceID, created.DelegatedRun.ID, f.target.ID
	accepted, err := f.store.AppendEvent(ctx, store.Event{
		Type: "delegation.workspace_accepted", ProjectID: f.project.ID, IssueID: &f.issue.ID,
		RunID: &childRunID, AgentID: &targetAgentID, WorkspaceID: &workspaceID,
		Actor: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = accepted
	message, err := f.store.AppendEvent(ctx, store.Event{
		Type: "agent.message", ProjectID: f.project.ID, IssueID: &f.issue.ID,
		RunID: &childRunID, AgentID: &targetAgentID, WorkspaceID: &workspaceID,
		Actor: store.EmptyObject, Payload: []byte(`{"message":"bounded delegated result"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	mutation, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease, RunStatus: "COMPLETED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Run.Status != "COMPLETED" {
		t.Fatalf("child status=%s want COMPLETED", mutation.Run.Status)
	}
	if len(mutation.Events) != 1 || mutation.Events[0].Type != "delegation.completed" || mutation.Events[0].RunID == nil || *mutation.Events[0].RunID != f.parentRun.ID {
		t.Fatalf("delegation completion events=%+v", mutation.Events)
	}

	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeSucceeded || delegation.ResultSummary == nil || *delegation.ResultSummary != "bounded delegated result" {
		t.Fatalf("delegation result=%+v", delegation)
	}
	if delegation.ResultEventID == nil || *delegation.ResultEventID != message.ID || delegation.WorkspaceChangesAccepted == nil || !*delegation.WorkspaceChangesAccepted {
		t.Fatalf("delegation evidence=%+v", delegation)
	}
	if delegation.ContinuationJobID == nil || delegation.CompletedAt == nil {
		t.Fatalf("delegation continuation state=%+v", delegation)
	}

	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "QUEUED" {
		t.Fatalf("parent status=%s want QUEUED", parent.Status)
	}
	var kind, state, runID, key string
	if err := f.store.pool.QueryRow(ctx, `
		SELECT kind, state, run_id::text, idempotency_key
		FROM scheduler_jobs WHERE project_id=$1 AND id=$2
	`, f.project.ID, *delegation.ContinuationJobID).Scan(&kind, &state, &runID, &key); err != nil {
		t.Fatal(err)
	}
	if kind != "RESUME" || state != "QUEUED" || runID != f.parentRun.ID || key != "delegation:"+delegation.ID+":resume" {
		t.Fatalf("continuation kind=%s state=%s run=%s key=%s", kind, state, runID, key)
	}
	var childReviews int
	if err := f.store.pool.QueryRow(ctx, `SELECT count(*) FROM reviews WHERE project_id=$1 AND run_id=$2`, f.project.ID, childRunID).Scan(&childReviews); err != nil {
		t.Fatal(err)
	}
	if childReviews != 0 {
		t.Fatalf("delegated child review count=%d want 0", childReviews)
	}
	issueAfter, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if issueAfter.Status != issueBefore.Status || issueAfter.AssigneeType == nil || issueBefore.AssigneeType == nil || *issueAfter.AssigneeType != *issueBefore.AssigneeType || issueAfter.AssigneeID == nil || issueBefore.AssigneeID == nil || *issueAfter.AssigneeID != *issueBefore.AssigneeID {
		t.Fatalf("delegated completion changed Issue authority: before=%+v after=%+v", issueBefore, issueAfter)
	}
}

func TestDelegatedCompletionRollsBackWhenContinuationCannotBeRecorded(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "result-rollback")
	ctx := t.Context()
	childRunID := created.DelegatedRun.ID
	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO scheduler_jobs (project_id, run_id, kind, state, idempotency_key)
		VALUES ($1, $2, 'RESUME', 'QUEUED', $3)
	`, f.project.ID, childRunID, "delegation:"+created.Delegation.ID+":resume"); err != nil {
		t.Fatal(err)
	}

	_, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease, RunStatus: "COMPLETED",
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("terminal transition err=%v want conflict", err)
	}
	child, err := f.store.GetRun(ctx, f.project.ID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "RUNNING" {
		t.Fatalf("child status=%s want RUNNING after rollback", child.Status)
	}
	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "PAUSED" {
		t.Fatalf("parent status=%s want PAUSED after rollback", parent.Status)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome != nil || delegation.CompletedAt != nil || delegation.ContinuationJobID != nil {
		t.Fatalf("delegation terminal state committed despite rollback: %+v", delegation)
	}
}

func prepareDelegatedChildForTerminal(t *testing.T, requestKey string) (delegationFixture, store.RequestDelegationResult, string, string) {
	t.Helper()
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	parentJobID, parentLease := claimDelegationParentJob(t, f)
	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "perform bounded delegated work", RequestKey: requestKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkDelegationWorkspaceHandoffReady(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, created.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: parentJobID, RunID: f.parentRun.ID, LeaseToken: parentLease, RunStatus: "PAUSED",
	}); err != nil {
		t.Fatal(err)
	}

	var childJobID string
	if err := f.store.pool.QueryRow(ctx, `
		UPDATE scheduler_jobs
		SET state='CLAIMED', wait_reason=NULL, updated_at=now()
		WHERE project_id=$1 AND run_id=$2 AND kind='START' AND state='QUEUED'
		RETURNING id::text
	`, f.project.ID, created.DelegatedRun.ID).Scan(&childJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='STARTING', started_at=now(), updated_at=now() WHERE project_id=$1 AND id=$2 AND status='QUEUED'`, f.project.ID, created.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}
	var childLease string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO scheduler_leases (job_id, owner_id, expires_at)
		VALUES ($1, 'delegation-result-test', now() + interval '5 minutes')
		RETURNING lease_token::text
	`, childJobID).Scan(&childLease); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: created.DelegatedRun.ID, LeaseToken: childLease, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatal(err)
	}
	return f, created, childJobID, childLease
}
