package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestExpiredClaimWithBlockingQuestionBecomesWaitingWithoutReplay(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "question-crash-reconcile")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "question-crash-reconcile")
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
	question, err := s.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		RunID:     f.run.ID,
		Prompt:    "Need a durable answer",
		Kind:      "TEXT",
		Blocking:  true,
		Status:    "OPEN",
	})
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	expireLease(t, s, admission.Job.ID)

	claim, err := s.ClaimExpiredJobForReconciliation(ctx, "reconciler", time.Minute)
	if err != nil {
		t.Fatalf("reconcile persisted question: %v", err)
	}
	if claim != nil {
		t.Fatalf("expected durable Question reconciliation to finish without Engine claim, got %+v", claim)
	}
	run, err := s.GetRun(ctx, f.project.ID, f.run.ID)
	if err != nil {
		t.Fatalf("read run: %v", err)
	}
	if run.Status != "WAITING_FOR_INPUT" {
		t.Fatalf("run status=%s want WAITING_FOR_INPUT", run.Status)
	}
	issue, err := s.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatalf("read issue: %v", err)
	}
	if issue.Status != "BLOCKED" {
		t.Fatalf("issue status=%s want BLOCKED", issue.Status)
	}
	persisted, err := s.GetQuestion(ctx, f.project.ID, question.ID)
	if err != nil || persisted.Status != "OPEN" {
		t.Fatalf("persisted question=%+v err=%v", persisted, err)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)

	var jobState string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, admission.Job.ID).Scan(&jobState); err != nil {
		t.Fatalf("read job state: %v", err)
	}
	if jobState != "DONE" {
		t.Fatalf("job state=%s want DONE", jobState)
	}
	readmitted, err := s.AdmitNextJob(ctx, "worker-new", time.Minute, time.Second)
	if err != nil {
		t.Fatalf("admit after reconciliation: %v", err)
	}
	if readmitted != nil {
		t.Fatalf("unexpected Engine replay admission: %+v", readmitted)
	}
}
