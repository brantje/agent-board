package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestTerminalInteractiveQuestionAppendsCancellationEvent(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "terminal-question-cancellation-event")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "terminal-question-cancellation-event-start")
	admission := mustAdmit(t, s, "terminal-question-cancellation-event-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	opened, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
		Question: store.Question{
			ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID,
			Prompt: "Cancel this native input", Kind: "TEXT", Blocking: true, Status: "OPEN",
		},
		Engine: "opencode", CorrelationKey: "terminal-cancel-event/0",
	})
	if err != nil {
		t.Fatalf("open interactive Question: %v", err)
	}

	reason := "native session failed"
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "FAILED", FailureReason: &reason,
	}); err != nil {
		t.Fatalf("terminal transition: %v", err)
	}

	var eventCount int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM events
		WHERE project_id=$1 AND run_id=$2 AND type='question.cancelled' AND payload->>'questionId'=$3
	`, f.project.ID, f.run.ID, opened.Question.ID).Scan(&eventCount); err != nil {
		t.Fatalf("count question.cancelled events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("question.cancelled events=%d want 1", eventCount)
	}
}

func TestExpiredInteractiveClaimRecoversAnsweredWaitingRun(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "answered-waiting-expired-claim")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "answered-waiting-expired-claim-start")
	admission := mustAdmit(t, s, "answered-waiting-expired-claim-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	opened, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
		Question: store.Question{
			ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID,
			Prompt: "Continue after the claim expires?", Kind: "TEXT", Blocking: true, Status: "OPEN",
		},
		Engine: "opencode", CorrelationKey: "answered-expired/0",
	})
	if err != nil {
		t.Fatalf("open interactive Question: %v", err)
	}

	answer := "continue"
	answered, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID: f.project.ID, QuestionID: opened.Question.ID,
		Answer: store.QuestionAnswer{Kind: "TEXT", Text: &answer}, ActorType: "HUMAN",
	})
	if err != nil {
		t.Fatalf("answer with live claim: %v", err)
	}
	if answered.Run.Status != "WAITING_FOR_INPUT" || answered.Job != nil {
		t.Fatalf("live answer result=%+v", answered)
	}

	// Recreate the durable state left by the lease race: the answer committed
	// while the old execution claim expired before it could resume the run.
	expireLease(t, s, admission.Job.ID)
	claim, err := s.ClaimExpiredJobForReconciliation(ctx, "answered-waiting-reconciler", time.Minute)
	if err != nil {
		t.Fatalf("reconcile answered waiting run: %v", err)
	}
	if claim != nil {
		t.Fatalf("expected local Question recovery without Engine replay, got %+v", claim)
	}

	run, err := s.GetRun(ctx, f.project.ID, f.run.ID)
	if err != nil {
		t.Fatalf("read run: %v", err)
	}
	if run.Status != "QUEUED" {
		t.Fatalf("run status=%s want QUEUED", run.Status)
	}
	assertIssueStatus(t, s, f.project.ID, f.issue.ID, "IN_PROGRESS")
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)

	var oldJobState string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE project_id=$1 AND id=$2`, f.project.ID, admission.Job.ID).Scan(&oldJobState); err != nil {
		t.Fatalf("read expired job state: %v", err)
	}
	if oldJobState != "DONE" {
		t.Fatalf("expired job state=%s want DONE", oldJobState)
	}

	var resumeCount int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM scheduler_jobs
		WHERE project_id=$1 AND run_id=$2 AND kind='RESUME' AND state='QUEUED' AND idempotency_key=$3
	`, f.project.ID, f.run.ID, "question:"+opened.Question.ID+":resume").Scan(&resumeCount); err != nil {
		t.Fatalf("count resume jobs: %v", err)
	}
	if resumeCount != 1 {
		t.Fatalf("resume jobs=%d want 1", resumeCount)
	}
}
