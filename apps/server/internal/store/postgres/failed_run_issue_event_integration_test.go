package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestFailedRunRecoveryReturnsExactlyPersistedStatusEvent(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "failed-recovery-event-result")
	setFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	enqueueFixtureRun(t, s, f, f.run, "failed-recovery-event-result")
	admission := mustAdmit(t, s, "failed-recovery-event-result-worker")
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
	if len(result.Events) != 1 {
		t.Fatalf("mutation events=%d want 1", len(result.Events))
	}
	event := result.Events[0]
	if event.Type != "issue.status_changed" || event.RunID == nil || *event.RunID != f.run.ID ||
		event.AgentID == nil || *event.AgentID != f.agent.ID || event.WorkspaceID == nil || *event.WorkspaceID != f.workspace.ID {
		t.Fatalf("recovery event=%+v", event)
	}
	assertStatusEventPayload(t, event.Payload, "IN_PROGRESS", "TODO")

	var persisted int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM events
		WHERE id=$1 AND project_id=$2 AND issue_id=$3 AND type='issue.status_changed'
	`, event.ID, f.project.ID, f.issue.ID).Scan(&persisted); err != nil {
		t.Fatalf("count persisted recovery Event: %v", err)
	}
	if persisted != 1 {
		t.Fatalf("persisted recovery Event count=%d want 1", persisted)
	}
	assertFixtureIssueStatus(t, s, f, "TODO")
}

func TestFailedRunWithoutIssueRecoveryReturnsNoStatusEvent(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "failed-no-recovery-event")
	setFixtureIssueStatus(t, s, f, "BLOCKED")
	enqueueFixtureRun(t, s, f, f.run, "failed-no-recovery-event")
	admission := mustAdmit(t, s, "failed-no-recovery-event-worker")
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

func TestFailedReconciliationReturnsPersistedRecoveryStatusEvent(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "failed-reconciliation-event-result")
	setFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	enqueueFixtureRun(t, s, f, f.run, "failed-reconciliation-event-result")
	admission := mustAdmit(t, s, "failed-reconciliation-event-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	expireLease(t, s, admission.Job.ID)
	reconciled := mustClaimReconciliation(t, s, "failed-reconciliation-event-reconciler")
	failure := "external execution failed"
	result, err := s.ResolveReconciliationMutation(ctx, store.SchedulerReconciliation{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: reconciled.Lease.LeaseToken, Outcome: store.SchedulerReconciliationFailed, FailureReason: &failure,
	})
	if err != nil {
		t.Fatalf("failed reconciliation mutation: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("reconciliation events=%d want 1", len(result.Events))
	}
	event := result.Events[0]
	if event.Type != "issue.status_changed" || event.RunID == nil || *event.RunID != f.run.ID ||
		event.AgentID == nil || *event.AgentID != f.agent.ID || event.WorkspaceID == nil || *event.WorkspaceID != f.workspace.ID {
		t.Fatalf("reconciliation recovery event=%+v", event)
	}
	assertStatusEventPayload(t, event.Payload, "IN_PROGRESS", "TODO")
	assertFixtureIssueStatus(t, s, f, "TODO")
	assertIssueStatusEventCount(t, s, f, 1)
}
