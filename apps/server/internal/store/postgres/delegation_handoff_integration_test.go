package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestDelegationWorkspaceHandoffDefersAndReleasesSchedulerJob(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "change the current workspace", RequestKey: "handoff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.SchedulerJob.WaitReason == nil || *created.SchedulerJob.WaitReason != store.DelegationWorkspaceHandoffWaitReason {
		t.Fatalf("scheduler wait reason=%v", created.SchedulerJob.WaitReason)
	}
	if created.DelegatedRun.QueueReason == nil || *created.DelegatedRun.QueueReason != store.DelegationWorkspaceHandoffWaitReason {
		t.Fatalf("run queue reason=%v", created.DelegatedRun.QueueReason)
	}
	if !created.SchedulerJob.AvailableAt.After(time.Now().AddDate(50, 0, 0)) {
		t.Fatalf("delegated job is runnable before handoff: availableAt=%s", created.SchedulerJob.AvailableAt)
	}

	if err := f.store.CompleteDelegationWorkspaceHandoff(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, created.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}
	var waitReason *string
	var availableAt time.Time
	if err := f.store.pool.QueryRow(ctx, `
		SELECT wait_reason, available_at
		FROM scheduler_jobs
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, created.SchedulerJob.ID).Scan(&waitReason, &availableAt); err != nil {
		t.Fatal(err)
	}
	if waitReason != nil {
		t.Fatalf("scheduler wait reason=%q want nil", *waitReason)
	}
	if availableAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("delegated job was not released: availableAt=%s", availableAt)
	}
	run, err := f.store.GetRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.QueueReason != nil {
		t.Fatalf("run queue reason=%q want nil", *run.QueueReason)
	}

	// A recovery replay after the durable release is harmless.
	if err := f.store.CompleteDelegationWorkspaceHandoff(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, created.DelegatedRun.ID); err != nil {
		t.Fatalf("idempotent handoff completion: %v", err)
	}
}

func TestDelegationWorkspaceHandoffFailsClosedAfterParentStops(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "change the current workspace", RequestKey: "cancelled-handoff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE runs SET status='CANCELLED', completed_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CompleteDelegationWorkspaceHandoff(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, created.DelegatedRun.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("handoff err=%v want conflict", err)
	}

	var waitReason *string
	if err := f.store.pool.QueryRow(ctx, `SELECT wait_reason FROM scheduler_jobs WHERE project_id=$1 AND id=$2`, f.project.ID, created.SchedulerJob.ID).Scan(&waitReason); err != nil {
		t.Fatal(err)
	}
	if waitReason == nil || *waitReason != store.DelegationWorkspaceHandoffWaitReason {
		t.Fatalf("failed handoff released child job: waitReason=%v", waitReason)
	}
}

func TestDelegationWorkspaceHandoffRejectsSubstitutedChild(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "change the current workspace", RequestKey: "substitution",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.CompleteDelegationWorkspaceHandoff(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, f.parentRun.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("substituted child err=%v want not found", err)
	}
}
