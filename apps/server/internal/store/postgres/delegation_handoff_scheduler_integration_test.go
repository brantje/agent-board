package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerPausedAtomicallyReleasesReadyDelegationHandoff(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	parentJobID, leaseToken := claimDelegationParentJob(t, f)
	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "inherit the synchronized workspace", RequestKey: "atomic-pause",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkDelegationWorkspaceHandoffReady(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, created.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: parentJobID, RunID: f.parentRun.ID, LeaseToken: leaseToken, RunStatus: "PAUSED",
	}); err != nil {
		t.Fatal(err)
	}

	var waitReason *string
	var availableAt time.Time
	if err := f.store.pool.QueryRow(ctx, `
		SELECT wait_reason, available_at FROM scheduler_jobs WHERE project_id=$1 AND id=$2
	`, f.project.ID, created.SchedulerJob.ID).Scan(&waitReason, &availableAt); err != nil {
		t.Fatal(err)
	}
	if waitReason != nil {
		t.Fatalf("child wait reason=%q want nil", *waitReason)
	}
	if availableAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("child job not released with PAUSED transition: availableAt=%s", availableAt)
	}
	child, err := f.store.GetRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.QueueReason != nil {
		t.Fatalf("child queue reason=%q want nil", *child.QueueReason)
	}
}

func TestSchedulerCancellationDoesNotReleaseReadyDelegationHandoff(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	parentJobID, leaseToken := claimDelegationParentJob(t, f)
	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "must not outlive parent authority", RequestKey: "cancel-ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkDelegationWorkspaceHandoffReady(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, created.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: parentJobID, RunID: f.parentRun.ID, LeaseToken: leaseToken, RunStatus: "CANCELLED",
	}); err != nil {
		t.Fatal(err)
	}

	var waitReason *string
	var availableAt time.Time
	if err := f.store.pool.QueryRow(ctx, `
		SELECT wait_reason, available_at FROM scheduler_jobs WHERE project_id=$1 AND id=$2
	`, f.project.ID, created.SchedulerJob.ID).Scan(&waitReason, &availableAt); err != nil {
		t.Fatal(err)
	}
	if waitReason == nil || *waitReason != store.DelegationWorkspaceHandoffReadyReason {
		t.Fatalf("cancelled parent released child: waitReason=%v", waitReason)
	}
	if !availableAt.After(time.Now().AddDate(50, 0, 0)) {
		t.Fatalf("cancelled parent made child runnable: availableAt=%s", availableAt)
	}
}



func TestDelegationHandoffReadySurvivesStoreRestart(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	parentJobID, leaseToken := claimDelegationParentJob(t, f)
	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "resume handoff after restart", RequestKey: "restart-ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkDelegationWorkspaceHandoffReady(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, created.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}

	restarted := New(f.store.pool)
	var waitReason *string
	var availableAt time.Time
	if err := restarted.pool.QueryRow(ctx, `
		SELECT wait_reason, available_at
		FROM scheduler_jobs
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, created.SchedulerJob.ID).Scan(&waitReason, &availableAt); err != nil {
		t.Fatal(err)
	}
	if waitReason == nil || *waitReason != store.DelegationWorkspaceHandoffReadyReason || !availableAt.After(time.Now().AddDate(50, 0, 0)) {
		t.Fatalf("restart lost held handoff state: waitReason=%v availableAt=%s", waitReason, availableAt)
	}

	if _, err := restarted.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: parentJobID, RunID: f.parentRun.ID, LeaseToken: leaseToken, RunStatus: "PAUSED",
	}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.pool.QueryRow(ctx, `
		SELECT wait_reason, available_at
		FROM scheduler_jobs
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, created.SchedulerJob.ID).Scan(&waitReason, &availableAt); err != nil {
		t.Fatal(err)
	}
	if waitReason != nil || availableAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("restart did not release durable handoff after parent pause: waitReason=%v availableAt=%s", waitReason, availableAt)
	}
}

func TestDelegationHandoffTransfersRunnerWorkspaceOwnershipToChild(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	parentJobID, leaseToken := claimDelegationParentJob(t, f)

	runner, err := f.store.CreateRunner(ctx, store.Runner{Name: "delegation-handoff-owner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	parentSession, err := f.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID:     f.parentRun.ID,
		RunnerID:  runner.ID,
		Status:    "PENDING",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID:    f.project.ID,
		SessionID:    parentSession.ID,
		FromStatuses: []string{"PENDING"},
		Status:       "COMPLETED",
	}); err != nil {
		t.Fatal(err)
	}

	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "continue from the handed-off workspace", RequestKey: "runner-owner-transfer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkDelegationWorkspaceHandoffReady(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, created.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: parentJobID, RunID: f.parentRun.ID, LeaseToken: leaseToken, RunStatus: "PAUSED",
	}); err != nil {
		t.Fatal(err)
	}

	if lock, err := f.store.AcquireWorkspaceBootstrapLock(ctx, f.parentRun.WorkspaceID); !errors.Is(err, store.ErrConflict) {
		if lock != nil {
			_ = lock.Release()
		}
		t.Fatalf("generic writer crossed paused handoff boundary: %v", err)
	}

	childSession, err := f.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID:     created.DelegatedRun.ID,
		RunnerID:  runner.ID,
		Status:    "PENDING",
	})
	if err != nil {
		t.Fatalf("create delegated child session: %v", err)
	}
	childLock, err := f.store.AcquireWorkspaceExecutionLock(ctx, f.parentRun.WorkspaceID, childSession.ID)
	if err != nil {
		t.Fatalf("delegated child did not take Workspace ownership: %v", err)
	}
	if err := childLock.Release(); err != nil {
		t.Fatalf("release delegated child Workspace lock: %v", err)
	}
}

func claimDelegationParentJob(t *testing.T, f delegationFixture) (string, string) {
	t.Helper()
	ctx := t.Context()
	var jobID string
	if err := f.store.pool.QueryRow(ctx, `
		UPDATE scheduler_jobs
		SET state='CLAIMED', wait_reason=NULL, updated_at=now()
		WHERE project_id=$1 AND run_id=$2 AND kind='START' AND state='QUEUED'
		RETURNING id::text
	`, f.project.ID, f.parentRun.ID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	var leaseToken string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO scheduler_leases (job_id, owner_id, expires_at)
		VALUES ($1, 'delegation-handoff-test', now() + interval '5 minutes')
		RETURNING lease_token::text
	`, jobID).Scan(&leaseToken); err != nil {
		t.Fatal(err)
	}
	return jobID, leaseToken
}


type delegationRecoverySchedulerStore struct {
	store.SchedulerStore
	cancel context.CancelFunc
}

func (s delegationRecoverySchedulerStore) ResolveReconciliation(ctx context.Context, input store.SchedulerReconciliation) (store.Run, error) {
	run, err := s.SchedulerStore.ResolveReconciliation(ctx, input)
	if err == nil && s.cancel != nil {
		s.cancel()
	}
	return run, err
}

type delegationRecoveryProcessor struct{}

func (delegationRecoveryProcessor) Process(context.Context, *store.SchedulerAdmission, scheduler.Lifecycle) (scheduler.Result, error) {
	return scheduler.Result{}, fmt.Errorf("delegation recovery unexpectedly started a scheduler worker")
}

type delegationRecoveryReconciler struct {
	calls int
}

func (r *delegationRecoveryReconciler) Reconcile(_ context.Context, claim *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error) {
	r.calls++
	if claim == nil || claim.Run.Status != "RUNNING" {
		return store.SchedulerReconciliationUnknown, nil, fmt.Errorf("unexpected reconciliation claim: %+v", claim)
	}
	return store.SchedulerReconciliationUnknown, nil, nil
}

func TestSchedulerReconciliationRecoversReadyDelegationHandoffAfterRestart(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()

	// Re-enter the fixture through ordinary scheduler admission so the parent
	// owns the Workspace through a real claimed START job and lease.
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE runs
		SET status='QUEUED', started_at=NULL, updated_at=now()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	runner, err := f.store.CreateRunner(ctx, store.Runner{Name: "delegation recovery runner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.ObserveRunner(ctx, runner.ID, []byte(`{"max_active_sessions":1}`)); err != nil {
		t.Fatal(err)
	}
	f.store.SetRunnerCandidates(func(string) []string { return []string{runner.ID} })

	claim, err := f.store.AdmitNextJob(ctx, "delegation-recovery-before-restart", time.Minute, time.Millisecond)
	if err != nil || claim == nil {
		t.Fatalf("admit parent claim=%+v err=%v", claim, err)
	}
	if claim.Run.ID != f.parentRun.ID || claim.Run.WorkspaceID != f.parentRun.WorkspaceID {
		t.Fatalf("parent admission=%+v want Run %s Workspace %s", claim.Run, f.parentRun.ID, f.parentRun.WorkspaceID)
	}
	parent, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID,
		JobID: claim.Job.ID,
		RunID: f.parentRun.ID,
		LeaseToken: claim.Lease.LeaseToken,
		RunStatus: "RUNNING",
	})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "RUNNING" {
		t.Fatalf("parent status=%s want RUNNING", parent.Status)
	}

	parentSession, err := f.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID: f.parentRun.ID,
		RunnerID: runner.ID,
		Status: "PENDING",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: f.project.ID,
		SessionID: parentSession.ID,
		FromStatuses: []string{"PENDING"},
		Status: "COMPLETED",
	}); err != nil {
		t.Fatal(err)
	}

	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID,
		ParentRunID: f.parentRun.ID,
		TargetAgentID: f.target.ID,
		Task: "recover the existing delegated handoff",
		RequestKey: "restart-reconciliation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.SchedulerJob.WaitReason == nil || *created.SchedulerJob.WaitReason != store.DelegationWorkspaceHandoffWaitReason {
		t.Fatalf("delegated child was not initially held: %+v", created.SchedulerJob)
	}
	if err := f.store.MarkDelegationWorkspaceHandoffReady(
		ctx,
		f.project.ID,
		f.parentRun.ID,
		created.Delegation.ID,
		created.DelegatedRun.ID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.pool.Exec(ctx, `
		UPDATE scheduler_leases
		SET acquired_at=now() - interval '2 seconds',
		    expires_at=now() - interval '1 second'
		WHERE job_id=$1 AND lease_token=$2
	`, claim.Job.ID, claim.Lease.LeaseToken); err != nil {
		t.Fatal(err)
	}

	restarted := New(f.store.pool)
	restarted.SetRunnerCandidates(func(string) []string { return []string{runner.ID} })
	reconcileCtx, cancel := context.WithCancel(context.Background())
	reconciler := &delegationRecoveryReconciler{}
	wrapped := delegationRecoverySchedulerStore{SchedulerStore: restarted, cancel: cancel}
	config := scheduler.DefaultConfig("delegation-recovery-after-restart")
	config.PollInterval = 5 * time.Millisecond
	config.LeaseDuration = time.Minute
	config.HeartbeatInterval = time.Second
	config.CapacityBackoff = time.Millisecond
	config.MaxInFlight = 1
	coordinator, err := scheduler.New(wrapped, delegationRecoveryProcessor{}, reconciler, config)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- coordinator.Run(reconcileCtx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("restarted coordinator: %v", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("timed out waiting for restart reconciliation")
	}
	if reconciler.calls != 1 {
		t.Fatalf("reconciliation calls=%d want 1", reconciler.calls)
	}

	parent, err = restarted.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "PAUSED" {
		t.Fatalf("recovered parent status=%s want PAUSED", parent.Status)
	}

	var childJobState string
	var childWaitReason *string
	var childAvailableAt time.Time
	if err := restarted.pool.QueryRow(ctx, `
		SELECT state, wait_reason, available_at
		FROM scheduler_jobs
		WHERE project_id=$1 AND id=$2 AND run_id=$3 AND kind='START'
	`, f.project.ID, created.SchedulerJob.ID, created.DelegatedRun.ID).Scan(&childJobState, &childWaitReason, &childAvailableAt); err != nil {
		t.Fatal(err)
	}
	if childJobState != "QUEUED" || childWaitReason != nil || childAvailableAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("recovered child job state=%s wait=%v availableAt=%s", childJobState, childWaitReason, childAvailableAt)
	}
	child, err := restarted.GetRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "QUEUED" || child.QueueReason != nil || child.WorkspaceID != parent.WorkspaceID {
		t.Fatalf("recovered child=%+v parent=%+v", child, parent)
	}

	delegations, err := restarted.ListDelegationsByParentRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(delegations) != 1 || delegations[0].ID != created.Delegation.ID || delegations[0].DelegatedRunID != created.DelegatedRun.ID {
		t.Fatalf("restart reconciliation duplicated delegation: %+v", delegations)
	}
	var childJobs int
	if err := restarted.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM scheduler_jobs
		WHERE project_id=$1 AND run_id=$2 AND kind='START'
	`, f.project.ID, created.DelegatedRun.ID).Scan(&childJobs); err != nil {
		t.Fatal(err)
	}
	if childJobs != 1 {
		t.Fatalf("delegated START jobs=%d want 1", childJobs)
	}

	var parentClaims, liveParentSessions int
	if err := restarted.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM scheduler_jobs WHERE project_id=$1 AND run_id=$2 AND state='CLAIMED'),
			(SELECT count(*) FROM execution_sessions WHERE project_id=$1 AND run_id=$2 AND status IN ('PENDING','STARTING','RUNNING'))
	`, f.project.ID, f.parentRun.ID).Scan(&parentClaims, &liveParentSessions); err != nil {
		t.Fatal(err)
	}
	if parentClaims != 0 || liveParentSessions != 0 {
		t.Fatalf("parent retained authoritative ownership after handoff: claims=%d liveSessions=%d", parentClaims, liveParentSessions)
	}

	again, err := restarted.ClaimExpiredJobForReconciliation(ctx, "delegation-recovery-repeat", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if again != nil {
		t.Fatalf("repeated reconciliation reclaimed completed parent handoff: %+v", again)
	}
}
