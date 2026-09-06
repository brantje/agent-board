package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerWaitingPreservesDoneIssue(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "waiting-preserves-done")
	enqueueFixtureRun(t, s, f, f.run, "waiting-preserves-done")
	admission := mustAdmit(t, s, "worker-waiting-preserves-done")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE issues SET status='DONE' WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID); err != nil {
		t.Fatalf("mark issue done: %v", err)
	}

	run, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "WAITING_FOR_INPUT",
	})
	if err != nil {
		t.Fatalf("transition waiting: %v", err)
	}
	if run.Status != "WAITING_FOR_INPUT" {
		t.Fatalf("run status=%s want WAITING_FOR_INPUT", run.Status)
	}
	var issueStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM issues WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID).Scan(&issueStatus); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if issueStatus != "DONE" {
		t.Fatalf("issue status=%s want DONE", issueStatus)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)
}

func TestBlockingQuestionReconciliationPreservesDoneIssue(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "reconcile-question-preserves-done")
	enqueueFixtureRun(t, s, f, f.run, "reconcile-question-preserves-done")
	admission := mustAdmit(t, s, "worker-reconcile-question-preserves-done")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      admission.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if _, err := s.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		RunID:     f.run.ID,
		Prompt:    "Need input",
		Kind:      "TEXT",
		Blocking:  true,
		Status:    "OPEN",
	}); err != nil {
		t.Fatalf("create blocking question: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE issues SET status='DONE' WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID); err != nil {
		t.Fatalf("mark issue done: %v", err)
	}
	expireLease(t, s, admission.Job.ID)

	reconciliation, err := s.ClaimExpiredJobForReconciliation(ctx, "reconciler", time.Minute)
	if err != nil {
		t.Fatalf("reconcile blocking question: %v", err)
	}
	if reconciliation != nil {
		t.Fatalf("blocking question should resolve stale claim internally: %+v", reconciliation)
	}
	run, err := s.GetRun(ctx, f.project.ID, f.run.ID)
	if err != nil {
		t.Fatalf("read run: %v", err)
	}
	if run.Status != "WAITING_FOR_INPUT" {
		t.Fatalf("run status=%s want WAITING_FOR_INPUT", run.Status)
	}
	var issueStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM issues WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID).Scan(&issueStatus); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if issueStatus != "DONE" {
		t.Fatalf("issue status=%s want DONE", issueStatus)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)
}
