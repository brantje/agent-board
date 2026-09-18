package postgres

import (
	"errors"
	"testing"
	"time"

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
