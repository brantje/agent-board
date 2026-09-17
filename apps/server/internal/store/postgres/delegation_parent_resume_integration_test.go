package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestDelegatedReadyForReviewQueuesPausedParentResume(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	parentJobID, parentLease := claimDelegationParentJob(t, f)
	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "write the bounded change", RequestKey: "resume-parent",
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

	childJobID, childLease := claimDelegationChildJob(t, f, created.DelegatedRun.ID)
	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: created.DelegatedRun.ID, LeaseToken: childLease, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE workspaces SET base_revision='base', current_revision='delegate-head', updated_at=now()
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.parentRun.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childJobID, RunID: created.DelegatedRun.ID, LeaseToken: childLease, RunStatus: "READY_FOR_REVIEW",
	}); err != nil {
		t.Fatal(err)
	}

	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "QUEUED" || parent.WorkspaceID != created.DelegatedRun.WorkspaceID {
		t.Fatalf("parent after delegate completion=%+v", parent)
	}
	var count int
	var state, kind, key string
	if err := f.store.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(state), ''), COALESCE(max(kind), ''), COALESCE(max(idempotency_key), '')
		FROM scheduler_jobs
		WHERE project_id=$1 AND run_id=$2 AND kind='RESUME'
	`, f.project.ID, f.parentRun.ID).Scan(&count, &state, &kind, &key); err != nil {
		t.Fatal(err)
	}
	if count != 1 || state != "QUEUED" || kind != "RESUME" || key != "delegation:"+created.Delegation.ID+":resume-parent" {
		t.Fatalf("parent resume count=%d state=%q kind=%q key=%q", count, state, kind, key)
	}
}

func claimDelegationChildJob(t *testing.T, f delegationFixture, runID string) (string, string) {
	t.Helper()
	ctx := t.Context()
	var jobID string
	if err := f.store.pool.QueryRow(ctx, `
		UPDATE scheduler_jobs
		SET state='CLAIMED', wait_reason=NULL, updated_at=now()
		WHERE project_id=$1 AND run_id=$2 AND kind='START' AND state='QUEUED' AND available_at <= now()
		RETURNING id::text
	`, f.project.ID, runID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE runs SET status='STARTING', queue_reason=NULL, updated_at=now() WHERE project_id=$1 AND id=$2 AND status='QUEUED'`, f.project.ID, runID); err != nil {
		t.Fatal(err)
	}
	var leaseToken string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO scheduler_leases (job_id, owner_id, expires_at)
		VALUES ($1, 'delegation-parent-resume-test', now() + interval '5 minutes')
		RETURNING lease_token::text
	`, jobID).Scan(&leaseToken); err != nil {
		t.Fatal(err)
	}
	return jobID, leaseToken
}
