package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerAdmissionValidationAndEmptyQueue(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		owner    string
		lease    time.Duration
		backoff  time.Duration
	}{
		{name: "blank owner", owner: "", lease: time.Minute, backoff: time.Second},
		{name: "zero lease", owner: "worker", lease: 0, backoff: time.Second},
		{name: "zero backoff", owner: "worker", lease: time.Minute, backoff: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.AdmitNextJob(ctx, tc.owner, tc.lease, tc.backoff); !errors.Is(err, store.ErrInvalidArgument) {
				t.Fatalf("error=%v want invalid argument", err)
			}
		})
	}
	admission, err := s.AdmitNextJob(ctx, "worker", time.Minute, time.Second)
	if err != nil || admission != nil {
		t.Fatalf("empty queue admission=%+v err=%v", admission, err)
	}
}

func TestSchedulerTransitionValidationKeepsOwnership(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "validation-transition")
	enqueueFixtureRun(t, s, f, f.run, "validation-transition")
	admission := mustAdmit(t, s, "worker")

	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank transition error=%v", err)
	}
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: admission.Run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "QUEUED",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("invalid transition error=%v", err)
	}
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: admission.Run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "FAILED",
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("missing failure reason error=%v", err)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)
}

func TestSchedulerReconciliationValidationAndNoExpiredClaim(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	if _, err := s.ClaimExpiredJobForReconciliation(ctx, "", time.Minute); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank owner error=%v", err)
	}
	if _, err := s.ClaimExpiredJobForReconciliation(ctx, "worker", 0); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("zero lease error=%v", err)
	}
	claim, err := s.ClaimExpiredJobForReconciliation(ctx, "worker", time.Minute)
	if err != nil || claim != nil {
		t.Fatalf("empty reconciliation claim=%+v err=%v", claim, err)
	}
	if _, err := s.ResolveReconciliation(ctx, store.SchedulerReconciliation{}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank reconciliation error=%v", err)
	}
}

func TestSchedulerReconciliationActiveKeepsOwnership(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "validation-active")
	enqueueFixtureRun(t, s, f, f.run, "validation-active")
	admission := mustAdmit(t, s, "old-worker")
	expireLease(t, s, admission.Job.ID)
	claim := mustClaimReconciliation(t, s, "reconciler")

	run, err := s.ResolveReconciliation(ctx, store.SchedulerReconciliation{
		ProjectID: f.project.ID, JobID: claim.Job.ID, RunID: claim.Run.ID,
		LeaseToken: claim.Lease.LeaseToken, Outcome: store.SchedulerReconciliationActive,
	})
	if err != nil {
		t.Fatalf("active reconciliation: %v", err)
	}
	if run.Status != "STARTING" {
		t.Fatalf("run status=%s want STARTING", run.Status)
	}
	assertSchedulerOwnershipCounts(t, s, claim.Job.ID, 1, 2)
}

func TestSchedulerReconciliationCompletedAndCancelledReleaseOwnership(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outcome  store.SchedulerReconciliationOutcome
		status   string
		jobState string
	}{
		{name: "completed", outcome: store.SchedulerReconciliationCompleted, status: "COMPLETED", jobState: "DONE"},
		{name: "cancelled", outcome: store.SchedulerReconciliationCancelled, status: "CANCELLED", jobState: "CANCELLED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(testPool(t))
			ctx := context.Background()
			f := seedRunFixture(t, s, "validation-"+tc.name)
			enqueueFixtureRun(t, s, f, f.run, "validation-"+tc.name)
			admission := mustAdmit(t, s, "old-worker")
			expireLease(t, s, admission.Job.ID)
			claim := mustClaimReconciliation(t, s, "reconciler")

			run, err := s.ResolveReconciliation(ctx, store.SchedulerReconciliation{
				ProjectID: f.project.ID, JobID: claim.Job.ID, RunID: claim.Run.ID,
				LeaseToken: claim.Lease.LeaseToken, Outcome: tc.outcome,
			})
			if err != nil {
				t.Fatalf("resolve %s: %v", tc.name, err)
			}
			if run.Status != tc.status || run.CompletedAt == nil {
				t.Fatalf("run=%+v want status %s completed", run, tc.status)
			}
			assertSchedulerOwnershipCounts(t, s, claim.Job.ID, 0, 0)
			var state string
			if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, claim.Job.ID).Scan(&state); err != nil {
				t.Fatalf("read job state: %v", err)
			}
			if state != tc.jobState {
				t.Fatalf("job state=%s want %s", state, tc.jobState)
			}
		})
	}
}

func TestSchedulerReconciliationRejectsUnsafeOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome store.SchedulerReconciliationOutcome
		reason  *string
	}{
		{name: "missing outcome", outcome: ""},
		{name: "failed without reason", outcome: store.SchedulerReconciliationFailed},
		{name: "unknown enum", outcome: store.SchedulerReconciliationOutcome("BOGUS")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(testPool(t))
			ctx := context.Background()
			f := seedRunFixture(t, s, "validation-unsafe-"+tc.name)
			enqueueFixtureRun(t, s, f, f.run, "validation-unsafe-"+tc.name)
			admission := mustAdmit(t, s, "old-worker")
			expireLease(t, s, admission.Job.ID)
			claim := mustClaimReconciliation(t, s, "reconciler")

			_, err := s.ResolveReconciliation(ctx, store.SchedulerReconciliation{
				ProjectID: f.project.ID, JobID: claim.Job.ID, RunID: claim.Run.ID,
				LeaseToken: claim.Lease.LeaseToken, Outcome: tc.outcome, FailureReason: tc.reason,
			})
			if !errors.Is(err, store.ErrInvalidArgument) {
				t.Fatalf("error=%v want invalid argument", err)
			}
			assertSchedulerOwnershipCounts(t, s, claim.Job.ID, 1, 2)
		})
	}
}
