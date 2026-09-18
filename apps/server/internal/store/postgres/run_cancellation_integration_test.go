package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCancelInactiveRunCancelsQueuedSchedulerWorkAndPersistsEvent(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='QUEUED', started_at=NULL, updated_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	result, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != "CANCELLED" || result.Event.Type != "run.cancelled" || result.Event.RunID == nil || *result.Event.RunID != f.parentRun.ID {
		t.Fatalf("cancellation result=%+v", result)
	}
	var queued, cancelled, events int
	if err := f.store.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM scheduler_jobs WHERE project_id=$1 AND run_id=$2 AND state='QUEUED'),
		  (SELECT count(*) FROM scheduler_jobs WHERE project_id=$1 AND run_id=$2 AND state='CANCELLED'),
		  (SELECT count(*) FROM events WHERE project_id=$1 AND run_id=$2 AND type='run.cancelled')
	`, f.project.ID, f.parentRun.ID).Scan(&queued, &cancelled, &events); err != nil {
		t.Fatal(err)
	}
	if queued != 0 || cancelled != 1 || events != 1 {
		t.Fatalf("queued=%d cancelled=%d events=%d", queued, cancelled, events)
	}
}


func TestCancelInactiveRunCancelsClaimedRunBeforeExecutionStarts(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	jobID, leaseToken := claimDelegationParentJob(t, f)

	result, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != "CANCELLED" {
		t.Fatalf("claimed Run status=%s want CANCELLED", result.Run.Status)
	}
	var state, currentStatus string
	if err := f.store.pool.QueryRow(ctx, "SELECT state FROM scheduler_jobs WHERE id=$1", jobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(ctx, "SELECT status FROM runs WHERE project_id=$1 AND id=$2", f.project.ID, f.parentRun.ID).Scan(&currentStatus); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" || currentStatus != "CANCELLED" {
		t.Fatalf("job state=%s Run status=%s", state, currentStatus)
	}
	assertSchedulerOwnershipCounts(t, f.store, jobID, 0, 0)

	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: jobID, RunID: f.parentRun.ID,
		LeaseToken: leaseToken, RunStatus: "RUNNING",
	}); err == nil {
		t.Fatal("stale admitted worker transitioned a durably cancelled Run")
	}
}

func TestCreateExecutionSessionRejectsDurablyCancelledClaim(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	_, _ = claimDelegationParentJob(t, f)
	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	projectID := f.project.ID
	runnerValue, err := f.store.CreateRunner(ctx, store.Runner{
		ProjectID: &projectID, Name: "post-cancel-session-runner", TokenHash: make([]byte, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: projectID, RunID: f.parentRun.ID, RunnerID: runnerValue.ID,
		Status: "PENDING", CWD: "/workspace", CommandArgv: []byte(`["agent"]`),
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Execution Session created after durable cancellation: err=%v want ErrNotFound", err)
	}
}

func TestCancelInactiveRunRefusesClaimedRunWithLiveExecutionSession(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	jobID, _ := claimDelegationParentJob(t, f)
	projectID := f.project.ID
	runnerValue, err := f.store.CreateRunner(ctx, store.Runner{
		ProjectID: &projectID, Name: "claimed-cancellation-runner", TokenHash: make([]byte, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: projectID, RunID: f.parentRun.ID, RunnerID: runnerValue.ID,
		Status: "PENDING", CWD: "/workspace", CommandArgv: []byte("[\"agent\"]"),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("claimed cancellation with external authority err=%v want conflict", err)
	}
	var state, runStatus string
	if err := f.store.pool.QueryRow(ctx, "SELECT state FROM scheduler_jobs WHERE id=$1", jobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(ctx, "SELECT status FROM runs WHERE project_id=$1 AND id=$2", f.project.ID, f.parentRun.ID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if state != "CLAIMED" || runStatus != "STARTING" {
		t.Fatalf("fail-closed ownership changed: job=%s Run=%s", state, runStatus)
	}
	assertSchedulerOwnershipCounts(t, f.store, jobID, 1, 3)
}

func TestCancelInactiveDelegatedChildFinalizesOutcomeAndResumesParent(t *testing.T) {
	f, created := prepareQueuedDelegatedChildForCancellation(t, "inactive-child-cancel")
	ctx := t.Context()

	result, err := f.store.CancelInactiveRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != "CANCELLED" {
		t.Fatalf("child status=%s want CANCELLED", result.Run.Status)
	}
	if len(result.Events) != 2 || result.Events[0].Type != "run.cancelled" || result.Events[1].Type != "delegation.cancelled" {
		t.Fatalf("cancellation events=%+v", result.Events)
	}

	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeCancelled || delegation.CompletedAt == nil || delegation.ResultSummary == nil || *delegation.ResultSummary != "Delegated task was cancelled." {
		t.Fatalf("delegation result=%+v", delegation)
	}
	if delegation.ContinuationJobID == nil {
		t.Fatalf("delegated cancellation did not create parent continuation: %+v", delegation)
	}
	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "QUEUED" {
		t.Fatalf("parent status=%s want QUEUED", parent.Status)
	}
	var state, kind, runID, key string
	if err := f.store.pool.QueryRow(ctx, `
		SELECT state, kind, run_id::text, idempotency_key
		FROM scheduler_jobs
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, *delegation.ContinuationJobID).Scan(&state, &kind, &runID, &key); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || kind != "RESUME" || runID != f.parentRun.ID || key != "delegation:"+delegation.ID+":resume" {
		t.Fatalf("continuation state=%s kind=%s run=%s key=%s", state, kind, runID, key)
	}
	var cancelledEvents int
	if err := f.store.pool.QueryRow(ctx, `
		SELECT count(*) FROM events
		WHERE project_id=$1 AND run_id=$2 AND type='delegation.cancelled'
	`, f.project.ID, f.parentRun.ID).Scan(&cancelledEvents); err != nil {
		t.Fatal(err)
	}
	if cancelledEvents != 1 {
		t.Fatalf("delegation.cancelled events=%d want 1", cancelledEvents)
	}
}

func TestCancelInactiveDelegatedChildPersistsOutcomeWithoutResumingTerminalParent(t *testing.T) {
	f, created := prepareQueuedDelegatedChildForCancellation(t, "inactive-child-terminal-parent")
	ctx := t.Context()
	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}

	result, err := f.store.CancelInactiveRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != "CANCELLED" || len(result.Events) != 2 || result.Events[1].Type != "delegation.cancelled" {
		t.Fatalf("child cancellation=%+v", result)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeCancelled || delegation.CompletedAt == nil || delegation.ContinuationJobID != nil {
		t.Fatalf("terminal-parent delegation result=%+v", delegation)
	}
	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED", parent.Status)
	}
	var continuations int
	if err := f.store.pool.QueryRow(ctx, `
		SELECT count(*) FROM scheduler_jobs
		WHERE project_id=$1 AND idempotency_key=$2
	`, f.project.ID, "delegation:"+delegation.ID+":resume").Scan(&continuations); err != nil {
		t.Fatal(err)
	}
	if continuations != 0 {
		t.Fatalf("terminal parent received %d continuation jobs", continuations)
	}
}

func prepareQueuedDelegatedChildForCancellation(t *testing.T, requestKey string) (delegationFixture, store.RequestDelegationResult) {
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
	child, err := f.store.GetRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "QUEUED" {
		t.Fatalf("child status=%s want QUEUED", child.Status)
	}
	return f, created
}

func TestSchedulerAdmissionCancelsQueuedDelegateAfterParentCancellationAcrossRestart(t *testing.T) {
	f, created := prepareQueuedDelegatedChildForCancellation(t, "restart-admission-fence")
	ctx := t.Context()
	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}

	admission, err := f.store.AdmitNextJob(ctx, "worker-after-restart", time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if admission != nil {
		t.Fatalf("cancelled-parent delegate was admitted: %+v", admission)
	}
	child, err := f.store.GetRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "CANCELLED" {
		t.Fatalf("child status=%s want CANCELLED", child.Status)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeCancelled || delegation.ContinuationJobID != nil {
		t.Fatalf("delegation after admission fence=%+v", delegation)
	}
	var queuedStarts int
	if err := f.store.pool.QueryRow(ctx, `
		SELECT count(*) FROM scheduler_jobs
		WHERE project_id=$1 AND run_id=$2 AND kind='START' AND state='QUEUED'
	`, f.project.ID, child.ID).Scan(&queuedStarts); err != nil {
		t.Fatal(err)
	}
	if queuedStarts != 0 {
		t.Fatalf("queued delegated START jobs=%d want 0", queuedStarts)
	}
}


func TestClaimedDelegatedChildCancellationAfterParentCancellationDoesNotResumeParent(t *testing.T) {
	f, created := prepareQueuedDelegatedChildForCancellation(t, "claimed-child-cancel-race")
	ctx := t.Context()
	claim, err := f.store.AdmitNextJob(ctx, "claimed-child-worker", time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || claim.Run.ID != created.DelegatedRun.ID || claim.Run.Status != "STARTING" {
		t.Fatalf("delegated child admission=%+v", claim)
	}

	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, created.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}
	child, err := f.store.GetRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "CANCELLED" {
		t.Fatalf("child status=%s want CANCELLED", child.Status)
	}
	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED", parent.Status)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeCancelled || delegation.ContinuationJobID != nil {
		t.Fatalf("cancelled claimed delegation=%+v", delegation)
	}
	assertSchedulerOwnershipCounts(t, f.store, claim.Job.ID, 0, 0)
	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: claim.Job.ID, RunID: child.ID,
		LeaseToken: claim.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err == nil {
		t.Fatal("stale delegated worker entered RUNNING after cancellation")
	}
}

func TestReconciliationRetryCancelsDelegateWhenParentBecameTerminal(t *testing.T) {
	f, created, childJobID, childLease := prepareDelegatedChildForTerminal(t, "restart-reconciliation-fence")
	ctx := t.Context()
	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	expireDelegationSchedulerLease(t, f.store, childJobID, childLease)
	claim, err := f.store.ClaimExpiredJobForReconciliation(ctx, "worker-after-restart", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || claim.Run.ID != created.DelegatedRun.ID {
		t.Fatalf("reconciliation claim=%+v", claim)
	}

	mutation, err := f.store.ResolveReconciliationMutation(ctx, store.SchedulerReconciliation{
		ProjectID: f.project.ID, JobID: claim.Job.ID, RunID: claim.Run.ID, LeaseToken: claim.Lease.LeaseToken,
		Outcome: store.SchedulerReconciliationRetry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Run.Status != "CANCELLED" {
		t.Fatalf("reconciled child status=%s want CANCELLED", mutation.Run.Status)
	}
	delegation, err := f.store.GetDelegationByRun(ctx, f.project.ID, claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Outcome == nil || *delegation.Outcome != store.DelegationOutcomeCancelled || delegation.ContinuationJobID != nil {
		t.Fatalf("reconciled delegation=%+v", delegation)
	}
	var state string
	if err := f.store.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE project_id=$1 AND id=$2`, f.project.ID, claim.Job.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" {
		t.Fatalf("reconciled scheduler job state=%s want CANCELLED", state)
	}
}
