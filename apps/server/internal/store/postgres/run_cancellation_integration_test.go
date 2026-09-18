package postgres

import (
	"errors"
	"testing"

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

func TestCancelInactiveRunRefusesClaimedOrExecutingRun(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	jobID, _ := claimDelegationParentJob(t, f)
	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("claimed cancellation err=%v want conflict", err)
	}
	var state string
	if err := f.store.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, jobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "CLAIMED" {
		t.Fatalf("claimed job state=%s", state)
	}
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
