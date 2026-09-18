package postgres

import (
	"errors"
	"strings"
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
	var childJobState string
	if err := f.store.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE project_id=$1 AND id=$2`, f.project.ID, childJobID).Scan(&childJobState); err != nil {
		t.Fatal(err)
	}
	if childJobState != "DONE" {
		t.Fatalf("delegated child scheduler job state=%s want DONE", childJobState)
	}
	var childLeases, childReservations int
	if err := f.store.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_leases WHERE job_id=$1`, childJobID).Scan(&childLeases); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_capacity_reservations WHERE project_id=$1 AND run_id=$2`, f.project.ID, childRunID).Scan(&childReservations); err != nil {
		t.Fatal(err)
	}
	if childLeases != 0 || childReservations != 0 {
		t.Fatalf("delegated child retained scheduler ownership: leases=%d reservations=%d", childLeases, childReservations)
	}
	issueAfter, err := f.store.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if issueAfter.Status != issueBefore.Status || issueAfter.AssigneeType == nil || issueBefore.AssigneeType == nil || *issueAfter.AssigneeType != *issueBefore.AssigneeType || issueAfter.AssigneeID == nil || issueBefore.AssigneeID == nil || *issueAfter.AssigneeID != *issueBefore.AssigneeID {
		t.Fatalf("delegated completion changed Issue authority: before=%+v after=%+v", issueBefore, issueAfter)
	}
}


func TestDelegatedCompletionIgnoresLaterReasoningMessageForResult(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "result-visible-message")
	ctx := t.Context()
	childRunID := created.DelegatedRun.ID
	workspaceID, targetAgentID := created.DelegatedRun.WorkspaceID, f.target.ID

	visible, err := f.store.AppendEvent(ctx, store.Event{
		Type: "agent.message", ProjectID: f.project.ID, IssueID: &f.issue.ID,
		RunID: &childRunID, AgentID: &targetAgentID, WorkspaceID: &workspaceID,
		Actor: store.EmptyObject, Payload: []byte(`{"message":"final delegated finding","kind":"message","source":"opencode"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := f.store.AppendEvent(ctx, store.Event{
		Type: "agent.message", ProjectID: f.project.ID, IssueID: &f.issue.ID,
		RunID: &childRunID, AgentID: &targetAgentID, WorkspaceID: &workspaceID,
		Actor: store.EmptyObject, Payload: []byte(`{"message":"later reasoning trace","kind":"reasoning","source":"opencode"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease, RunStatus: "COMPLETED",
	}); err != nil {
		t.Fatal(err)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.ResultSummary == nil || *delegation.ResultSummary != "final delegated finding" {
		t.Fatalf("delegation result summary=%v want visible message", delegation.ResultSummary)
	}
	if delegation.ResultEventID == nil || *delegation.ResultEventID != visible.ID {
		t.Fatalf("delegation result event=%v want visible message event %s", delegation.ResultEventID, visible.ID)
	}
	if *delegation.ResultEventID == reasoning.ID {
		t.Fatalf("delegation selected reasoning event %s as result", reasoning.ID)
	}
	if delegation.ContinuationJobID == nil {
		t.Fatalf("delegated completion did not create parent continuation: %+v", delegation)
	}
}

func TestDelegatedCompletionReconciliationCreatesParentContinuationExactlyOnce(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "result-reconciliation")
	ctx := t.Context()
	workspaceID, childRunID, targetAgentID := created.DelegatedRun.WorkspaceID, created.DelegatedRun.ID, f.target.ID
	if _, err := f.store.AppendEvent(ctx, store.Event{
		Type: "delegation.workspace_accepted", ProjectID: f.project.ID, IssueID: &f.issue.ID,
		RunID: &childRunID, AgentID: &targetAgentID, WorkspaceID: &workspaceID,
		Actor: store.EmptyObject,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AppendEvent(ctx, store.Event{
		Type: "agent.message", ProjectID: f.project.ID, IssueID: &f.issue.ID,
		RunID: &childRunID, AgentID: &targetAgentID, WorkspaceID: &workspaceID,
		Actor: store.EmptyObject, Payload: []byte(`{"message":"recovered bounded result"}`),
	}); err != nil {
		t.Fatal(err)
	}
	mutation, err := f.store.ResolveReconciliationMutation(ctx, store.SchedulerReconciliation{
		ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease,
		Outcome: store.SchedulerReconciliationCompleted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Run.Status != "COMPLETED" || len(mutation.Events) != 1 || mutation.Events[0].Type != "delegation.completed" {
		t.Fatalf("reconciliation mutation=%+v", mutation)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.ContinuationJobID == nil || delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeSucceeded {
		t.Fatalf("delegation=%+v", delegation)
	}
	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "QUEUED" {
		t.Fatalf("parent status=%s want QUEUED", parent.Status)
	}
	_, err = f.store.ResolveReconciliationMutation(ctx, store.SchedulerReconciliation{
		ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease,
		Outcome: store.SchedulerReconciliationCompleted,
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("repeated reconciliation err=%v want not found", err)
	}
	var continuations int
	if err := f.store.pool.QueryRow(ctx, `
		SELECT count(*) FROM scheduler_jobs
		WHERE project_id=$1 AND idempotency_key=$2
	`, f.project.ID, "delegation:"+delegation.ID+":resume").Scan(&continuations); err != nil {
		t.Fatal(err)
	}
	if continuations != 1 {
		t.Fatalf("continuation jobs=%d want 1", continuations)
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

func TestDelegatedFailureReturnsBoundedOutcomeAndResumesParent(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "result-failure")
	ctx := t.Context()
	childRunID := created.DelegatedRun.ID
	workspaceID, targetAgentID := created.DelegatedRun.WorkspaceID, f.target.ID
	failureReason := "delegate failed after bounded analysis"
	failureEvent, err := f.store.AppendEvent(ctx, store.Event{
		Type: "run.failed", ProjectID: f.project.ID, IssueID: &f.issue.ID,
		RunID: &childRunID, AgentID: &targetAgentID, WorkspaceID: &workspaceID,
		Actor: store.EmptyObject, Payload: []byte(`{"reason":"` + failureReason + `"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	mutation, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease,
		RunStatus: "FAILED", FailureReason: &failureReason,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Run.Status != "FAILED" || len(mutation.Events) != 1 || mutation.Events[0].Type != "delegation.failed" {
		t.Fatalf("delegated failure mutation=%+v", mutation)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeFailed || delegation.ResultSummary == nil || *delegation.ResultSummary != failureReason || delegation.ResultEventID == nil || *delegation.ResultEventID != failureEvent.ID {
		t.Fatalf("delegated failure result=%+v", delegation)
	}
	if delegation.ContinuationJobID == nil {
		t.Fatalf("delegated failure did not create parent continuation: %+v", delegation)
	}
	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "QUEUED" {
		t.Fatalf("parent status=%s want QUEUED after delegated failure", parent.Status)
	}
	if parent.FailureReason != nil {
		t.Fatalf("delegated failure leaked failure reason onto parent: %q", *parent.FailureReason)
	}
}

func TestDelegatedResultIsBoundedAndDoesNotCopyDetailedEvidence(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "result-bounded")
	ctx := t.Context()
	childRunID := created.DelegatedRun.ID
	workspaceID, targetAgentID := created.DelegatedRun.WorkspaceID, f.target.ID
	longSummary := strings.Repeat("x", store.MaxDelegationResultCharacters+256)
	message, err := f.store.AppendEvent(ctx, store.Event{
		Type: "agent.message", ProjectID: f.project.ID, IssueID: &f.issue.ID,
		RunID: &childRunID, AgentID: &targetAgentID, WorkspaceID: &workspaceID,
		Actor: store.EmptyObject, Payload: []byte(`{"message":"` + longSummary + `"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateRawOutputChunk(ctx, store.RawOutputChunk{
		ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: childRunID, Stream: "STDOUT", Sequence: 1,
		StorageRef: "evidence://detailed-child-log", SizeBytes: 37,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease, RunStatus: "COMPLETED",
	}); err != nil {
		t.Fatal(err)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.ResultSummary == nil || len([]rune(*delegation.ResultSummary)) != store.MaxDelegationResultCharacters {
		t.Fatalf("bounded result length=%d want %d", len([]rune(valueOrEmpty(delegation.ResultSummary))), store.MaxDelegationResultCharacters)
	}
	if delegation.ResultEventID == nil || *delegation.ResultEventID != message.ID {
		t.Fatalf("delegation evidence reference=%v want child message %s", delegation.ResultEventID, message.ID)
	}
	parentChunks, err := f.store.ListRawOutputChunks(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parentChunks) != 0 {
		t.Fatalf("delegation copied raw child output to parent: %+v", parentChunks)
	}
	parentEvents, err := f.store.ListRunEvents(ctx, f.project.ID, f.parentRun.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range parentEvents {
		if strings.Contains(string(event.Payload), "evidence://detailed-child-log") || strings.Contains(string(event.Payload), longSummary) {
			t.Fatalf("delegation duplicated detailed child evidence into parent event %s", event.Type)
		}
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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

func TestDelegatedTerminalTransitionRejectsTargetAgentLineageMismatch(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "target-agent-lineage-mismatch")
	ctx := t.Context()
	if _, err := f.store.pool.Exec(ctx, "UPDATE runs SET agent_id=$3 WHERE project_id=$1 AND id=$2", f.project.ID, created.DelegatedRun.ID, f.parent.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: created.DelegatedRun.ID,
		LeaseToken: childLease, RunStatus: "COMPLETED",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("terminal transition with corrupted target Agent error=%v want ErrConflict", err)
	}
	child, err := f.store.GetRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "RUNNING" {
		t.Fatalf("corrupted child terminal transition partially committed status=%s", child.Status)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome != nil || delegation.ContinuationJobID != nil {
		t.Fatalf("corrupted lineage finalized delegation=%+v", delegation)
	}
}

func TestDelegatedTerminalTransitionWaitsForExecutionSessionReconciliation(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "execution-session-uncertainty")
	ctx := t.Context()
	childRunID := created.DelegatedRun.ID
	projectID := f.project.ID

	runnerValue, err := f.store.CreateRunner(ctx, store.Runner{ProjectID: &projectID, Name: "delegation-uncertainty-runner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: projectID, RunID: childRunID, RunnerID: runnerValue.ID,
		Status: "PENDING", CWD: "/workspace", CommandArgv: []byte(`["agent"]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: projectID, SessionID: session.ID, FromStatuses: []string{"PENDING"}, Status: "STARTING",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: projectID, SessionID: session.ID, FromStatuses: []string{"STARTING"}, Status: "RUNNING",
	}); err != nil {
		t.Fatal(err)
	}

	reason := "runner transport disconnected"
	if _, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: projectID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease,
		RunStatus: "FAILED", FailureReason: &reason,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("terminal transition with live Execution Session error=%v want ErrConflict", err)
	}
	child, err := f.store.GetRun(ctx, projectID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "RUNNING" {
		t.Fatalf("child status=%s want RUNNING while execution is uncertain", child.Status)
	}
	assertSchedulerOwnershipCounts(t, f.store, childJobID, 1, 0)
	delegation, err := f.store.GetDelegationByRun(ctx, projectID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome != nil || delegation.ContinuationJobID != nil || delegation.CompletedAt != nil {
		t.Fatalf("delegation finalized while Execution Session was live: %+v", delegation)
	}
	if _, err := f.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: projectID, RunID: f.parentRun.ID, RunnerID: runnerValue.ID,
		Status: "PENDING", CWD: "/workspace", CommandArgv: []byte(`["parent"]`),
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("same-Workspace writer admission error=%v want ErrConflict", err)
	}

	if _, err := f.store.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: projectID, SessionID: session.ID, FromStatuses: []string{"RUNNING"}, Status: "FAILED",
	}); err != nil {
		t.Fatal(err)
	}
	expireLease(t, f.store, childJobID)
	reconciled := mustClaimReconciliation(t, f.store, "delegation-uncertainty-reconciler")
	mutation, err := f.store.ResolveReconciliationMutation(ctx, store.SchedulerReconciliation{
		ProjectID: projectID, JobID: childJobID, RunID: childRunID, LeaseToken: reconciled.Lease.LeaseToken,
		Outcome: store.SchedulerReconciliationFailed, FailureReason: &reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Run.Status != "FAILED" {
		t.Fatalf("reconciled child status=%s want FAILED", mutation.Run.Status)
	}
	delegation, err = f.store.GetDelegationByRun(ctx, projectID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeFailed || delegation.ContinuationJobID == nil {
		t.Fatalf("trusted terminal reconciliation did not finalize delegation: %+v", delegation)
	}
	assertSchedulerOwnershipCounts(t, f.store, childJobID, 0, 0)
}

func TestParentCancellationBeforeLateDelegateCompletionNeverQueuesContinuation(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "cancel-parent-first")
	ctx := t.Context()
	cancelled, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Run.Status != "CANCELLED" || cancelled.Event.Type != "run.cancelled" {
		t.Fatalf("parent cancellation=%+v", cancelled)
	}
	childRunID := created.DelegatedRun.ID
	mutation, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease, RunStatus: "COMPLETED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Run.Status != "COMPLETED" {
		t.Fatalf("child status=%s want COMPLETED", mutation.Run.Status)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeSucceeded || delegation.ContinuationJobID != nil {
		t.Fatalf("late delegated result=%+v", delegation)
	}
	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED", parent.Status)
	}
}

func TestParentCancellationAfterDelegateCompletionCancelsQueuedContinuation(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "cancel-after-child")
	ctx := t.Context()
	childRunID := created.DelegatedRun.ID
	if _, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease, RunStatus: "COMPLETED",
	}); err != nil {
		t.Fatal(err)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.ContinuationJobID == nil {
		t.Fatal("delegated completion did not queue parent continuation")
	}
	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := f.store.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE project_id=$1 AND id=$2`, f.project.ID, *delegation.ContinuationJobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" {
		t.Fatalf("continuation state=%s want CANCELLED", state)
	}
	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED", parent.Status)
	}
}

func TestDelegatedTerminalResultTrustsAuthoritativeRunnerHandbackWithoutConvenienceMarker(t *testing.T) {
	for _, direction := range []string{"from_runner", "git_publish"} {
		t.Run(direction, func(t *testing.T) {
			f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "accepted-handback-"+direction)
			ctx := t.Context()
			childRunID := created.DelegatedRun.ID
			workspaceID, targetAgentID := created.DelegatedRun.WorkspaceID, f.target.ID
			if _, err := f.store.AppendEvent(ctx, store.Event{
				Type: "workspace.transfer.completed", ProjectID: f.project.ID, IssueID: &f.issue.ID,
				RunID: &childRunID, AgentID: &targetAgentID, WorkspaceID: &workspaceID,
				Actor: store.EmptyObject, Payload: []byte(`{"direction":"` + direction + `"}`),
			}); err != nil {
				t.Fatal(err)
			}

			mutation, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
				ProjectID: f.project.ID, JobID: childJobID, RunID: childRunID, LeaseToken: childLease, RunStatus: "CANCELLED",
			})
			if err != nil {
				t.Fatal(err)
			}
			if mutation.Run.Status != "CANCELLED" {
				t.Fatalf("child status=%s want CANCELLED", mutation.Run.Status)
			}
			delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, childRunID)
			if err != nil {
				t.Fatal(err)
			}
			if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeCancelled || delegation.WorkspaceChangesAccepted == nil || !*delegation.WorkspaceChangesAccepted {
				t.Fatalf("delegation result=%+v", delegation)
			}
			var markerEvents int
			if err := f.store.pool.QueryRow(ctx, `
				SELECT count(*) FROM events
				WHERE project_id=$1 AND run_id=$2 AND type='delegation.workspace_accepted'
			`, f.project.ID, childRunID).Scan(&markerEvents); err != nil {
				t.Fatal(err)
			}
			if markerEvents != 0 {
				t.Fatalf("test unexpectedly wrote convenience marker: %d", markerEvents)
			}
		})
	}
}
