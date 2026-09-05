package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
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
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)
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
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)

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
