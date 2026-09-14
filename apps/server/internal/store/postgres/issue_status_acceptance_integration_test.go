package postgres

import (
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunStartAndCompletionDoNotProjectIssueStatus(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-run-independence")
	setFixtureIssueStatus(t, s, f, "BLOCKED")
	enqueueFixtureRun(t, s, f, f.run, "status-run-independence")

	admission := mustAdmit(t, s, "worker-status-run-independence")
	assertFixtureIssueStatus(t, s, f, "BLOCKED")
	assertIssueStatusEventCount(t, s, f, 0)

	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	}); err != nil {
		t.Fatalf("transition running: %v", err)
	}
	assertFixtureIssueStatus(t, s, f, "BLOCKED")
	assertIssueStatusEventCount(t, s, f, 0)

	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "COMPLETED",
	}); err != nil {
		t.Fatalf("transition completed: %v", err)
	}
	assertFixtureIssueStatus(t, s, f, "BLOCKED")
	assertIssueStatusEventCount(t, s, f, 0)
}

func TestAgentIssueStatusMutationPersistsCanonicalEventAttribution(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-agent-event")
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='RUNNING' WHERE project_id=$1 AND id=$2`, f.project.ID, f.run.ID); err != nil {
		t.Fatalf("set fixture Run status: %v", err)
	}
	actor, err := json.Marshal(map[string]string{"type": store.ActorTypeAgent, "id": f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		Status:      "IN_PROGRESS",
		Actor:       actor,
		RunID:       &f.run.ID,
		AgentID:     &f.agent.ID,
		WorkspaceID: &f.workspace.ID,
	})
	if err != nil {
		t.Fatalf("SetIssueStatus() error=%v", err)
	}
	if result.Issue.Status != "IN_PROGRESS" || len(result.Events) != 1 {
		t.Fatalf("mutation result=%+v", result)
	}
	event := result.Events[0]
	if event.Type != "issue.status_changed" || event.RunID == nil || *event.RunID != f.run.ID || event.AgentID == nil || *event.AgentID != f.agent.ID || event.WorkspaceID == nil || *event.WorkspaceID != f.workspace.ID {
		t.Fatalf("status event provenance=%+v", event)
	}
	var attributed struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(event.Actor, &attributed); err != nil {
		t.Fatalf("decode actor: %v", err)
	}
	if attributed.Type != store.ActorTypeAgent || attributed.ID != f.agent.ID {
		t.Fatalf("status event actor=%+v", attributed)
	}
	assertStatusEventPayload(t, event.Payload, "TODO", "IN_PROGRESS")
}

func TestFailedRunRecoveryMovesLastInProgressIssueToTodo(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-failed-recovery")
	setFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	enqueueFixtureRun(t, s, f, f.run, "status-failed-recovery")
	admission := mustAdmit(t, s, "worker-status-failed-recovery")

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
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:     f.project.ID,
		JobID:         admission.Job.ID,
		RunID:         f.run.ID,
		LeaseToken:    admission.Lease.LeaseToken,
		RunStatus:     "FAILED",
		FailureReason: &failure,
	}); err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	assertFixtureIssueStatus(t, s, f, "TODO")
	assertIssueStatusEventCount(t, s, f, 1)
	assertRecoveryStatusEvent(t, s, f)
}

func TestFailedRunRecoveryPreservesInProgressWhenAnotherRunIsActive(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-failed-concurrent")
	setFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	other, err := s.CreateRun(ctx, store.Run{
		ProjectID:   f.project.ID,
		IssueID:     f.issue.ID,
		WorkspaceID: f.workspace.ID,
		AgentID:     &f.agent.ID,
		Attempt:     f.run.Attempt + 1,
		Status:      "RUNNING",
	})
	if err != nil {
		t.Fatalf("create concurrent Run: %v", err)
	}
	if other.Status != "RUNNING" {
		t.Fatalf("concurrent Run status=%s want RUNNING", other.Status)
	}

	enqueueFixtureRun(t, s, f, f.run, "status-failed-concurrent")
	admission := mustAdmit(t, s, "worker-status-failed-concurrent")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	}); err != nil {
		t.Fatalf("transition running: %v", err)
	}
	failure := "first Run failed"
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:     f.project.ID,
		JobID:         admission.Job.ID,
		RunID:         f.run.ID,
		LeaseToken:    admission.Lease.LeaseToken,
		RunStatus:     "FAILED",
		FailureReason: &failure,
	}); err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	assertFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	assertIssueStatusEventCount(t, s, f, 0)
}

func TestReconciliationRetryDoesNotRollbackInProgressIssue(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-reconcile-retry")
	setFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	enqueueFixtureRun(t, s, f, f.run, "status-reconcile-retry")
	admission := mustAdmit(t, s, "worker-status-reconcile-retry")
	expireLease(t, s, admission.Job.ID)
	reconciled := mustClaimReconciliation(t, s, "reconciler-status-retry")

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
	if run.Status != "QUEUED" {
		t.Fatalf("reconciled Run status=%s want QUEUED", run.Status)
	}
	assertFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	assertIssueStatusEventCount(t, s, f, 0)
}

func TestFailedReconciliationRecoversLastInProgressIssueToTodo(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "status-reconcile-failed")
	setFixtureIssueStatus(t, s, f, "IN_PROGRESS")
	enqueueFixtureRun(t, s, f, f.run, "status-reconcile-failed")
	admission := mustAdmit(t, s, "worker-status-reconcile-failed")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	}); err != nil {
		t.Fatalf("transition running: %v", err)
	}
	expireLease(t, s, admission.Job.ID)
	reconciled := mustClaimReconciliation(t, s, "reconciler-status-failed")
	failure := "external execution failed"

	run, err := s.ResolveReconciliation(ctx, store.SchedulerReconciliation{
		ProjectID:     f.project.ID,
		JobID:         admission.Job.ID,
		RunID:         f.run.ID,
		LeaseToken:    reconciled.Lease.LeaseToken,
		Outcome:       store.SchedulerReconciliationFailed,
		FailureReason: &failure,
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if run.Status != "FAILED" {
		t.Fatalf("reconciled Run status=%s want FAILED", run.Status)
	}
	assertFixtureIssueStatus(t, s, f, "TODO")
	assertIssueStatusEventCount(t, s, f, 1)
	assertRecoveryStatusEvent(t, s, f)
}

func setFixtureIssueStatus(t *testing.T, s *Store, f runFixture, status string) {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), `UPDATE issues SET status=$3 WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID, status); err != nil {
		t.Fatalf("set fixture Issue status: %v", err)
	}
}

func assertFixtureIssueStatus(t *testing.T, s *Store, f runFixture, want string) {
	t.Helper()
	var got string
	if err := s.pool.QueryRow(t.Context(), `SELECT status FROM issues WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID).Scan(&got); err != nil {
		t.Fatalf("read Issue status: %v", err)
	}
	if got != want {
		t.Fatalf("Issue status=%s want %s", got, want)
	}
}

func assertIssueStatusEventCount(t *testing.T, s *Store, f runFixture, want int) {
	t.Helper()
	var got int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM events WHERE project_id=$1 AND issue_id=$2 AND type='issue.status_changed'`, f.project.ID, f.issue.ID).Scan(&got); err != nil {
		t.Fatalf("count Issue status events: %v", err)
	}
	if got != want {
		t.Fatalf("Issue status event count=%d want %d", got, want)
	}
}

func assertRecoveryStatusEvent(t *testing.T, s *Store, f runFixture) {
	t.Helper()
	var payload []byte
	var runID, agentID, workspaceID *string
	if err := s.pool.QueryRow(t.Context(), `
		SELECT payload, run_id::text, agent_id::text, workspace_id::text
		FROM events
		WHERE project_id=$1 AND issue_id=$2 AND type='issue.status_changed'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, f.project.ID, f.issue.ID).Scan(&payload, &runID, &agentID, &workspaceID); err != nil {
		t.Fatalf("read recovery status event: %v", err)
	}
	if runID == nil || *runID != f.run.ID || agentID == nil || *agentID != f.agent.ID || workspaceID == nil || *workspaceID != f.workspace.ID {
		t.Fatalf("recovery event provenance run=%v agent=%v workspace=%v", runID, agentID, workspaceID)
	}
	assertStatusEventPayload(t, payload, "IN_PROGRESS", "TODO")
}

func assertStatusEventPayload(t *testing.T, raw []byte, previous, current string) {
	t.Helper()
	var payload struct {
		PreviousStatus string `json:"previousStatus"`
		Status         string `json:"status"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode status event payload: %v", err)
	}
	if payload.PreviousStatus != previous || payload.Status != current {
		t.Fatalf("status event payload=%+v want previous=%s current=%s", payload, previous, current)
	}
}
