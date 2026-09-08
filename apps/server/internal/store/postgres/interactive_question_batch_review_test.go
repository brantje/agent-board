package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestInteractiveQuestionBatchRollsBackAllOnInsertFailure(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "interactive-question-batch-rollback")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "interactive-question-batch-rollback-start")
	admission := mustAdmit(t, s, "interactive-question-batch-rollback-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	_, err := s.OpenInteractiveQuestions(ctx, []store.OpenInteractiveQuestionCommand{
		{
			Question: store.Question{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: "First?", Kind: "TEXT", Blocking: true, Status: "OPEN"},
			Engine:   "opencode", CorrelationKey: "request-rollback:0",
		},
		{
			Question: store.Question{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: "Second?", Kind: "INVALID", Blocking: true, Status: "OPEN"},
			Engine:   "opencode", CorrelationKey: "request-rollback:1",
		},
	})
	if err == nil {
		t.Fatal("OpenInteractiveQuestions() unexpectedly succeeded")
	}

	var questions, bindings int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM questions WHERE project_id=$1 AND run_id=$2`, f.project.ID, f.run.ID).Scan(&questions); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM engine_question_bindings WHERE project_id=$1 AND run_id=$2`, f.project.ID, f.run.ID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if questions != 0 || bindings != 0 {
		t.Fatalf("partial batch persisted: questions=%d bindings=%d", questions, bindings)
	}

	var runStatus, issueStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM runs WHERE project_id=$1 AND id=$2`, f.project.ID, f.run.ID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT status FROM issues WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID).Scan(&issueStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "RUNNING" || issueStatus != "IN_PROGRESS" {
		t.Fatalf("batch failure changed lifecycle: run=%s issue=%s", runStatus, issueStatus)
	}
}

func TestInteractiveQuestionBatchCommitsTogetherAndEntersWaitingOnce(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "interactive-question-batch-commit")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "interactive-question-batch-commit-start")
	admission := mustAdmit(t, s, "interactive-question-batch-commit-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	batch, err := s.OpenInteractiveQuestions(ctx, []store.OpenInteractiveQuestionCommand{
		{
			Question: store.Question{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: "First?", Kind: "TEXT", Blocking: true, Status: "OPEN"},
			Engine:   "opencode", CorrelationKey: "request-commit:0",
		},
		{
			Question: store.Question{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: "Second?", Kind: "TEXT", Blocking: true, Status: "OPEN"},
			Engine:   "opencode", CorrelationKey: "request-commit:1",
		},
	})
	if err != nil {
		t.Fatalf("OpenInteractiveQuestions() error=%v", err)
	}
	if len(batch.Questions) != 2 || !batch.EnteredWaiting || batch.Run.Status != "WAITING_FOR_INPUT" {
		t.Fatalf("batch=%+v", batch)
	}
	entered := 0
	for _, result := range batch.Questions {
		if !result.Created || result.Question.ID == "" || result.Binding.QuestionID != result.Question.ID {
			t.Fatalf("result=%+v", result)
		}
		if result.EnteredWaiting {
			entered++
		}
	}
	if entered != 1 {
		t.Fatalf("entered waiting markers=%d want 1", entered)
	}
	assertIssueStatus(t, s, f.project.ID, f.issue.ID, "BLOCKED")
}

func TestExpiredInteractiveBatchWaitsForEveryAnswerBeforeRecovery(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "expired-interactive-batch")
	enqueueFixtureRun(t, s, f, f.run, "expired-interactive-batch-start")
	admission := mustAdmit(t, s, "expired-interactive-batch-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatal(err)
	}
	batch, err := s.OpenInteractiveQuestions(ctx, []store.OpenInteractiveQuestionCommand{
		{Question: store.Question{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: "First?", Kind: "TEXT", Blocking: true}, Engine: "opencode", CorrelationKey: "expired-batch/0"},
		{Question: store.Question{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: "Second?", Kind: "TEXT", Blocking: true}, Engine: "opencode", CorrelationKey: "expired-batch/1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	expireLease(t, s, admission.Job.ID)
	answer := "continue"
	for index, opened := range batch.Questions {
		result, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
			ProjectID: f.project.ID, QuestionID: opened.Question.ID,
			Answer: store.QuestionAnswer{Kind: "TEXT", Text: &answer}, ActorType: "HUMAN",
		})
		if err != nil {
			t.Fatalf("answer %d: %v", index, err)
		}
		if index == 0 {
			if result.Run.Status != "WAITING_FOR_INPUT" || result.Job != nil {
				t.Fatalf("first answer stranded the remaining Question: run=%s job=%+v", result.Run.Status, result.Job)
			}
			assertIssueStatus(t, s, f.project.ID, f.issue.ID, "BLOCKED")
		} else if result.Run.Status != "QUEUED" || result.Job == nil || result.Job.Kind != "RESUME" {
			t.Fatalf("final answer did not queue recovery: %+v", result)
		}
	}
	var resumes int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_jobs WHERE run_id=$1 AND kind='RESUME'`, f.run.ID).Scan(&resumes); err != nil {
		t.Fatal(err)
	}
	if resumes != 1 {
		t.Fatalf("resume jobs=%d want 1", resumes)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)
}
