package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerExpiredLeaseIsReconciledBeforeReuse(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "reconcile-takeover")
	enqueueFixtureRun(t, s, f, f.run, "reconcile-takeover")
	admission := mustAdmit(t, s, "worker-old")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	expireLease(t, s, admission.Job.ID)

	if _, err := s.RenewLease(ctx, f.project.ID, admission.Job.ID, admission.Lease.LeaseToken, time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired lease renewal error=%v want not found", err)
	}

	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "COMPLETED",
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired lease transition error=%v want not found", err)
	}

	reconciled, err := s.ClaimExpiredJobForReconciliation(ctx, "reconciler", time.Minute)
	if err != nil {
		t.Fatalf("claim reconciliation: %v", err)
	}
	if reconciled == nil {
		t.Fatal("expected reconciliation claim")
	}
	if reconciled.Lease.LeaseToken == admission.Lease.LeaseToken {
		t.Fatal("reconciliation must fence old lease with a new token")
	}
	if reconciled.Run.Status != "RUNNING" || reconciled.Job.State != "CLAIMED" {
		t.Fatalf("reconciled state run=%s job=%s", reconciled.Run.Status, reconciled.Job.State)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)
}

func TestSchedulerUnknownReconciliationKeepsCapacityReserved(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "reconcile-unknown")
	enqueueFixtureRun(t, s, f, f.run, "reconcile-unknown")
	admission := mustAdmit(t, s, "worker-old")
	expireLease(t, s, admission.Job.ID)
	reconciled := mustClaimReconciliation(t, s, "reconciler")

	run, err := s.ResolveReconciliation(ctx, store.SchedulerReconciliation{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: reconciled.Lease.LeaseToken,
		Outcome:    store.SchedulerReconciliationUnknown,
	})
	if err != nil {
		t.Fatalf("resolve unknown: %v", err)
	}
	if run.Status != "STARTING" {
		t.Fatalf("run status=%s want STARTING", run.Status)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)

	var jobState string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, admission.Job.ID).Scan(&jobState); err != nil {
		t.Fatalf("read job state: %v", err)
	}
	if jobState != "CLAIMED" {
		t.Fatalf("job state=%s want CLAIMED", jobState)
	}
}

func TestSchedulerRetryReconciliationReleasesAndRequeues(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "reconcile-retry")
	enqueueFixtureRun(t, s, f, f.run, "reconcile-retry")
	admission := mustAdmit(t, s, "worker-old")
	expireLease(t, s, admission.Job.ID)
	reconciled := mustClaimReconciliation(t, s, "reconciler")

	run, err := s.ResolveReconciliation(ctx, store.SchedulerReconciliation{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: reconciled.Lease.LeaseToken,
		Outcome:    store.SchedulerReconciliationRetry,
	})
	if err != nil {
		t.Fatalf("resolve retry: %v", err)
	}
	if run.Status != "QUEUED" || run.QueueReason == nil || *run.QueueReason != schedulerReconciliationRetryReason {
		t.Fatalf("run after retry=%+v", run)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)

	var jobState string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, admission.Job.ID).Scan(&jobState); err != nil {
		t.Fatalf("read job state: %v", err)
	}
	if jobState != "QUEUED" {
		t.Fatalf("job state=%s want QUEUED", jobState)
	}

	readmitted, err := s.AdmitNextJob(ctx, "worker-new", time.Minute, time.Second)
	if err != nil || readmitted == nil {
		t.Fatalf("readmission=%+v err=%v", readmitted, err)
	}
	if readmitted.Job.ID != admission.Job.ID {
		t.Fatalf("readmitted job=%s want %s", readmitted.Job.ID, admission.Job.ID)
	}
}

func TestSchedulerReconciliationTerminalOutcomes(t *testing.T) {
	reason := "external execution failed"
	blankReason := "   "
	cases := []struct {
		name              string
		outcome           store.SchedulerReconciliationOutcome
		failureReason     *string
		wantRunStatus     string
		wantJobState      string
		wantError         error
		wantLeaseCount    int
		wantReservationCount int
	}{
		{
			name:                 "completed",
			outcome:              store.SchedulerReconciliationCompleted,
			wantRunStatus:        "COMPLETED",
			wantJobState:         "DONE",
			wantLeaseCount:       0,
			wantReservationCount: 0,
		},
		{
			name:                 "cancelled",
			outcome:              store.SchedulerReconciliationCancelled,
			wantRunStatus:        "CANCELLED",
			wantJobState:         "CANCELLED",
			wantLeaseCount:       0,
			wantReservationCount: 0,
		},
		{
			name:                 "failed",
			outcome:              store.SchedulerReconciliationFailed,
			failureReason:        &reason,
			wantRunStatus:        "FAILED",
			wantJobState:         "FAILED",
			wantLeaseCount:       0,
			wantReservationCount: 0,
		},
		{
			name:                 "failed blank reason",
			outcome:              store.SchedulerReconciliationFailed,
			failureReason:        &blankReason,
			wantRunStatus:        "STARTING",
			wantJobState:         "CLAIMED",
			wantError:            store.ErrInvalidArgument,
			wantLeaseCount:       1,
			wantReservationCount: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(testPool(t))
			ctx := context.Background()
			f := seedRunFixture(t, s, "reconcile-terminal-"+tc.name)
			enqueueFixtureRun(t, s, f, f.run, "reconcile-terminal-"+tc.name)
			admission := mustAdmit(t, s, "worker-old")
			expireLease(t, s, admission.Job.ID)
			reconciled := mustClaimReconciliation(t, s, "reconciler")

			run, err := s.ResolveReconciliation(ctx, store.SchedulerReconciliation{
				ProjectID:     f.project.ID,
				JobID:         admission.Job.ID,
				RunID:         f.run.ID,
				LeaseToken:    reconciled.Lease.LeaseToken,
				Outcome:       tc.outcome,
				FailureReason: tc.failureReason,
			})
			if tc.wantError != nil {
				if !errors.Is(err, tc.wantError) {
					t.Fatalf("resolve error=%v want %v", err, tc.wantError)
				}
				run, err = s.GetRun(ctx, f.project.ID, f.run.ID)
				if err != nil {
					t.Fatalf("read run after rejected reconciliation: %v", err)
				}
			} else if err != nil {
				t.Fatalf("resolve %s: %v", tc.outcome, err)
			}
			if run.Status != tc.wantRunStatus {
				t.Fatalf("run status=%s want %s", run.Status, tc.wantRunStatus)
			}
			if tc.outcome == store.SchedulerReconciliationFailed && tc.wantError == nil {
				if run.CompletedAt == nil || run.FailureReason == nil || *run.FailureReason != reason {
					t.Fatalf("failed reconciliation run=%+v", run)
				}
			}
			assertSchedulerOwnershipCounts(t, s, admission.Job.ID, tc.wantLeaseCount, tc.wantReservationCount)

			var jobState string
			if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, admission.Job.ID).Scan(&jobState); err != nil {
				t.Fatalf("read job state: %v", err)
			}
			if jobState != tc.wantJobState {
				t.Fatalf("job state=%s want %s", jobState, tc.wantJobState)
			}
		})
	}
}

func TestSchedulerConcurrentReconciliationClaimsOnce(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "reconcile-race")
	enqueueFixtureRun(t, s, f, f.run, "reconcile-race")
	admission := mustAdmit(t, s, "worker-old")
	expireLease(t, s, admission.Job.ID)

	results := make(chan *store.SchedulerAdmission, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claim, err := s.ClaimExpiredJobForReconciliation(ctx, "reconciler", time.Minute)
			if err != nil {
				errs <- err
				return
			}
			results <- claim
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent reconciliation: %v", err)
	}

	claims := 0
	for claim := range results {
		if claim != nil {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("reconciliation claims=%d want 1", claims)
	}
}

func expireLease(t *testing.T, s *Store, jobID string) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `
		UPDATE scheduler_leases
		SET acquired_at=now() - interval '2 minutes', expires_at=now() - interval '1 minute'
		WHERE job_id=$1
	`, jobID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
}

func mustClaimReconciliation(t *testing.T, s *Store, owner string) *store.SchedulerAdmission {
	t.Helper()
	claim, err := s.ClaimExpiredJobForReconciliation(context.Background(), owner, time.Minute)
	if err != nil {
		t.Fatalf("claim reconciliation: %v", err)
	}
	if claim == nil {
		t.Fatal("expected reconciliation claim")
	}
	return claim
}
