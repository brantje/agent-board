package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func TestSchedulerTransitionKeepsCapacityWhileRunning(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "transition-running")
	enqueueFixtureRun(t, s, f, f.run, "transition-running")
	admission := mustAdmit(t, s, "worker-running")

	run, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	})
	if err != nil {
		t.Fatalf("transition running: %v", err)
	}
	if run.Status != "RUNNING" {
		t.Fatalf("run status=%s want RUNNING", run.Status)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 3)
}

func TestSchedulerTransitionReleasesCapacityOnInactiveStates(t *testing.T) {
	statuses := []struct {
		status   string
		jobState string
	}{
		{status: "WAITING_FOR_INPUT", jobState: "DONE"},
		{status: "PAUSED", jobState: "DONE"},
		{status: "READY_FOR_REVIEW", jobState: "DONE"},
		{status: "COMPLETED", jobState: "DONE"},
		{status: "FAILED", jobState: "FAILED"},
		{status: "CANCELLED", jobState: "CANCELLED"},
	}

	for i, tc := range statuses {
		t.Run(tc.status, func(t *testing.T) {
			s := New(testPool(t))
			ctx := context.Background()
			suffix := fmt.Sprintf("transition-%d", i)
			f := seedRunFixture(t, s, suffix)
			enqueueFixtureRun(t, s, f, f.run, suffix)
			admission := mustAdmit(t, s, "worker-"+suffix)

			if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
				ProjectID:  f.project.ID,
				JobID:      admission.Job.ID,
				RunID:      f.run.ID,
				LeaseToken: admission.Lease.LeaseToken,
				RunStatus:  "RUNNING",
			}); err != nil {
				t.Fatalf("transition running: %v", err)
			}

			failure := "engine failed"
			transition := store.SchedulerTransition{
				ProjectID:  f.project.ID,
				JobID:      admission.Job.ID,
				RunID:      f.run.ID,
				LeaseToken: admission.Lease.LeaseToken,
				RunStatus:  tc.status,
			}
			if tc.status == "FAILED" {
				transition.FailureReason = &failure
			}
			run, err := s.TransitionAdmittedJob(ctx, transition)
			if err != nil {
				t.Fatalf("transition %s: %v", tc.status, err)
			}
			if run.Status != tc.status {
				t.Fatalf("run status=%s want %s", run.Status, tc.status)
			}
			if tc.status == "FAILED" && (run.FailureReason == nil || *run.FailureReason != failure) {
				t.Fatalf("failure reason=%v want %q", run.FailureReason, failure)
			}
			assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)

			var jobState string
			if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, admission.Job.ID).Scan(&jobState); err != nil {
				t.Fatalf("read job state: %v", err)
			}
			if jobState != tc.jobState {
				t.Fatalf("job state=%s want %s", jobState, tc.jobState)
			}
		})
	}
}

func TestSchedulerReadyForReviewPreservesDoneIssueAndReleasesOwnership(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "ready-review-done-issue")
	enqueueFixtureRun(t, s, f, f.run, "ready-review-done-issue")
	admission := mustAdmit(t, s, "worker-ready-review-done-issue")

	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	}); err != nil {
		t.Fatalf("transition running: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE issues SET status='DONE' WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID); err != nil {
		t.Fatalf("mark issue done: %v", err)
	}

	run, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "READY_FOR_REVIEW",
	})
	if err != nil {
		t.Fatalf("transition ready for review: %v", err)
	}
	if run.Status != "READY_FOR_REVIEW" {
		t.Fatalf("run status=%s want READY_FOR_REVIEW", run.Status)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)

	var issueStatus, jobState string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM issues WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID).Scan(&issueStatus); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if issueStatus != "DONE" {
		t.Fatalf("issue status=%s want DONE", issueStatus)
	}
	if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, admission.Job.ID).Scan(&jobState); err != nil {
		t.Fatalf("read job state: %v", err)
	}
	if jobState != "DONE" {
		t.Fatalf("job state=%s want DONE", jobState)
	}

	var reviewStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM reviews WHERE run_id=$1`, f.run.ID).Scan(&reviewStatus); err != nil {
		t.Fatalf("read review: %v", err)
	}
	if reviewStatus != "PENDING" {
		t.Fatalf("review status=%s want PENDING", reviewStatus)
	}
}

func TestSchedulerReadyForReviewRejectsUnfinishedDelegationUntilContinuation(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	parentJobID, parentLease := claimDelegationParentJob(t, f)
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE workspaces
		SET base_revision='base-review-guard', current_revision='current-review-guard'
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.parentRun.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	created, err := f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
		ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
		Task: "complete before parent review", RequestKey: "ready-review-guard",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: parentJobID, RunID: f.parentRun.ID,
		LeaseToken: parentLease, RunStatus: "READY_FOR_REVIEW",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("READY_FOR_REVIEW with unfinished delegation error=%v want ErrConflict", err)
	}
	parent, err := f.store.GetRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != "RUNNING" {
		t.Fatalf("guard partially transitioned parent status=%s want RUNNING", parent.Status)
	}
	var reviews int
	if err := f.store.pool.QueryRow(ctx, `SELECT count(*) FROM reviews WHERE run_id=$1`, f.parentRun.ID).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if reviews != 0 {
		t.Fatalf("premature Review count=%d want 0", reviews)
	}

	if err := f.store.MarkDelegationWorkspaceHandoffReady(ctx, f.project.ID, f.parentRun.ID, created.Delegation.ID, created.DelegatedRun.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: parentJobID, RunID: f.parentRun.ID,
		LeaseToken: parentLease, RunStatus: "PAUSED",
	}); err != nil {
		t.Fatal(err)
	}

	childAdmission, err := f.store.AdmitNextJob(ctx, "review-guard-child", time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if childAdmission == nil || childAdmission.Run.ID != created.DelegatedRun.ID {
		t.Fatalf("child admission=%+v want %s", childAdmission, created.DelegatedRun.ID)
	}
	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childAdmission.Job.ID, RunID: childAdmission.Run.ID,
		LeaseToken: childAdmission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: childAdmission.Job.ID, RunID: childAdmission.Run.ID,
		LeaseToken: childAdmission.Lease.LeaseToken, RunStatus: "COMPLETED",
	}); err != nil {
		t.Fatal(err)
	}
	persisted, err := f.store.GetDelegationByRun(ctx, f.project.ID, created.DelegatedRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Outcome == nil || *persisted.Outcome != store.DelegationOutcomeSucceeded || persisted.ContinuationJobID == nil {
		t.Fatalf("delegation did not terminalize normally: %+v", persisted)
	}

	parentAdmission, err := f.store.AdmitNextJob(ctx, "review-guard-parent", time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if parentAdmission == nil || parentAdmission.Run.ID != f.parentRun.ID || parentAdmission.Job.Kind != "RESUME" {
		t.Fatalf("parent continuation admission=%+v", parentAdmission)
	}
	if _, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: parentAdmission.Job.ID, RunID: parentAdmission.Run.ID,
		LeaseToken: parentAdmission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatal(err)
	}
	final, err := f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: parentAdmission.Job.ID, RunID: parentAdmission.Run.ID,
		LeaseToken: parentAdmission.Lease.LeaseToken, RunStatus: "READY_FOR_REVIEW",
	})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "READY_FOR_REVIEW" {
		t.Fatalf("final parent status=%s want READY_FOR_REVIEW", final.Status)
	}
	if err := f.store.pool.QueryRow(ctx, `SELECT count(*) FROM reviews WHERE run_id=$1`, f.parentRun.ID).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if reviews != 1 {
		t.Fatalf("final Review count=%d want 1", reviews)
	}
}

func TestSchedulerReadyForReviewRaceWithDelegationRequestCannotCommitBoth(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	parentJobID, parentLease := claimDelegationParentJob(t, f)
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE workspaces
		SET base_revision='base-review-race', current_revision='current-review-race'
		WHERE project_id=$1 AND id=$2
	`, f.project.ID, f.parentRun.WorkspaceID); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var readyErr, delegationErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, readyErr = f.store.TransitionAdmittedJob(ctx, store.SchedulerTransition{
			ProjectID: f.project.ID, JobID: parentJobID, RunID: f.parentRun.ID,
			LeaseToken: parentLease, RunStatus: "READY_FOR_REVIEW",
		})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, delegationErr = f.store.RequestDelegation(ctx, store.RequestDelegationCommand{
			ProjectID: f.project.ID, ParentRunID: f.parentRun.ID, TargetAgentID: f.target.ID,
			Task: "race parent review", RequestKey: "ready-review-race",
		})
	}()
	close(start)
	wg.Wait()

	if (readyErr == nil) == (delegationErr == nil) {
		t.Fatalf("race results readyErr=%v delegationErr=%v want exactly one success", readyErr, delegationErr)
	}
	if readyErr != nil && !errors.Is(readyErr, store.ErrConflict) {
		t.Fatalf("READY_FOR_REVIEW race error=%v want ErrConflict or nil", readyErr)
	}
	if delegationErr != nil && !errors.Is(delegationErr, store.ErrConflict) {
		t.Fatalf("delegation race error=%v want ErrConflict or nil", delegationErr)
	}

	var reviews, unfinished int
	if err := f.store.pool.QueryRow(ctx, `SELECT count(*) FROM reviews WHERE run_id=$1`, f.parentRun.ID).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(ctx, `
		SELECT count(*) FROM delegations
		WHERE project_id=$1 AND parent_run_id=$2 AND outcome IS NULL
	`, f.project.ID, f.parentRun.ID).Scan(&unfinished); err != nil {
		t.Fatal(err)
	}
	if reviews == 1 && unfinished != 0 {
		t.Fatalf("race committed Review with unfinished delegation: reviews=%d unfinished=%d", reviews, unfinished)
	}
	if unfinished == 1 && reviews != 0 {
		t.Fatalf("race committed unfinished delegation with Review: reviews=%d unfinished=%d", reviews, unfinished)
	}
}

func TestCreatePendingReviewIsIdempotentAndRejectsConflictingExistingReview(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "pending-review-idempotency")
	prepareFixtureWorkspaceRevisions(t, s, f.workspace.ID, "pending-review-idempotency")

	create := func() error {
		t.Helper()
		tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatalf("begin transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := createPendingReview(ctx, tx, f.run); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	if err := create(); err != nil {
		t.Fatalf("first createPendingReview() error=%v", err)
	}
	if err := create(); err != nil {
		t.Fatalf("idempotent createPendingReview() error=%v", err)
	}

	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM reviews WHERE run_id=$1`, f.run.ID).Scan(&count); err != nil {
		t.Fatalf("count reviews: %v", err)
	}
	if count != 1 {
		t.Fatalf("review count=%d want 1", count)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE reviews SET status='APPROVED' WHERE run_id=$1`, f.run.ID); err != nil {
		t.Fatalf("corrupt review status: %v", err)
	}
	if err := create(); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflicting createPendingReview() error=%v want ErrConflict", err)
	}
}

func TestSchedulerTransitionRejectsStaleLeaseToken(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "transition-stale")
	enqueueFixtureRun(t, s, f, f.run, "transition-stale")
	admission := mustAdmit(t, s, "worker-stale")

	_, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: "00000000-0000-0000-0000-000000000000",
		RunStatus:  "COMPLETED",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale lease error=%v want not found", err)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 3)

	run, err := s.GetRun(ctx, f.project.ID, f.run.ID)
	if err != nil {
		t.Fatalf("get run after stale transition: %v", err)
	}
	if run.Status != "STARTING" {
		t.Fatalf("run status=%s want STARTING", run.Status)
	}
}

func mustAdmit(t *testing.T, s *Store, owner string) *store.SchedulerAdmission {
	t.Helper()
	admission, err := s.AdmitNextJob(context.Background(), owner, time.Minute, time.Second)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admission == nil {
		t.Fatal("expected scheduler admission")
	}
	return admission
}

func assertSchedulerOwnershipCounts(t *testing.T, s *Store, jobID string, wantLeases, wantReservations int) {
	t.Helper()
	ctx := context.Background()
	var leases, reservations int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_leases WHERE job_id=$1`, jobID).Scan(&leases); err != nil {
		t.Fatalf("count leases: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_capacity_reservations WHERE job_id=$1`, jobID).Scan(&reservations); err != nil {
		t.Fatalf("count reservations: %v", err)
	}
	if leases != wantLeases || reservations != wantReservations {
		t.Fatalf("ownership counts leases=%d reservations=%d want leases=%d reservations=%d", leases, reservations, wantLeases, wantReservations)
	}
}
