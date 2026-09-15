package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestFailedRunPreservesExplicitIssueStatus(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "failed-status-independent")
	setFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	enqueueFixtureRun(t, s, f, f.run, "failed-status-independent")
	admission := mustAdmit(t, s, "failed-status-independent-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	failure := "engine failed"
	result, err := s.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "FAILED", FailureReason: &failure,
	})
	if err != nil {
		t.Fatalf("failed transition mutation: %v", err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("mutation events=%+v want no Issue status Events", result.Events)
	}
	if result.Run.Status != "FAILED" {
		t.Fatalf("Run status=%s want FAILED", result.Run.Status)
	}
	assertFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	assertIssueStatusEventCount(t, s, f, 0)
}

func TestFailedRunPreservesOtherExplicitIssueStatus(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "failed-blocked-independent")
	setFixtureIssueStatus(t, s, f, "BLOCKED")
	enqueueFixtureRun(t, s, f, f.run, "failed-blocked-independent")
	admission := mustAdmit(t, s, "failed-blocked-independent-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	failure := "engine failed"
	result, err := s.TransitionAdmittedJobMutation(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "FAILED", FailureReason: &failure,
	})
	if err != nil {
		t.Fatalf("failed transition mutation: %v", err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("mutation events=%+v want none", result.Events)
	}
	assertFixtureIssueStatus(t, s, f, "BLOCKED")
	assertIssueStatusEventCount(t, s, f, 0)
}

func TestFailedReconciliationPreservesExplicitIssueStatus(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "failed-reconciliation-status-independent")
	setFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	enqueueFixtureRun(t, s, f, f.run, "failed-reconciliation-status-independent")
	admission := mustAdmit(t, s, "failed-reconciliation-status-independent-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	expireLease(t, s, admission.Job.ID)
	reconciled := mustClaimReconciliation(t, s, "failed-reconciliation-status-independent-reconciler")
	failure := "external execution failed"
	result, err := s.ResolveReconciliationMutation(ctx, store.SchedulerReconciliation{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: reconciled.Lease.LeaseToken, Outcome: store.SchedulerReconciliationFailed, FailureReason: &failure,
	})
	if err != nil {
		t.Fatalf("failed reconciliation mutation: %v", err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("reconciliation events=%+v want no Issue status Events", result.Events)
	}
	if result.Run.Status != "FAILED" {
		t.Fatalf("Run status=%s want FAILED", result.Run.Status)
	}
	assertFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	assertIssueStatusEventCount(t, s, f, 0)
}
