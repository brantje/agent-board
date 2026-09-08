package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestInteractiveQuestionKeepsClaimAndResumesInFlightRun(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "interactive-question")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "interactive-question-start")
	admission := mustAdmit(t, s, "interactive-question-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	opened, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
		Question: store.Question{
			ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID,
			Prompt: "Choose the native path", Kind: "TEXT", Blocking: true, Status: "OPEN",
		},
		Engine: "opencode", CorrelationKey: "session-1:request-1:0",
	})
	if err != nil {
		t.Fatalf("open interactive question: %v", err)
	}
	if !opened.Created || !opened.EnteredWaiting || opened.Run.Status != "WAITING_FOR_INPUT" || opened.Binding.State != store.InteractiveQuestionOpen {
		t.Fatalf("opened=%+v", opened)
	}
	assertIssueStatus(t, s, f.project.ID, f.issue.ID, "BLOCKED")
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)

	replayed, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
		Question: store.Question{
			ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID,
			Prompt: "ignored duplicate", Kind: "TEXT", Blocking: true, Status: "OPEN",
		},
		Engine: "opencode", CorrelationKey: "session-1:request-1:0",
	})
	if err != nil {
		t.Fatalf("reopen interactive question: %v", err)
	}
	if replayed.Created || replayed.EnteredWaiting || replayed.Question.ID != opened.Question.ID {
		t.Fatalf("replayed=%+v", replayed)
	}

	answer := "keep the native session"
	answered, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID: f.project.ID, QuestionID: opened.Question.ID,
		Answer: store.QuestionAnswer{Kind: "TEXT", Text: &answer}, ActorType: "HUMAN",
	})
	if err != nil {
		t.Fatalf("answer interactive question: %v", err)
	}
	if answered.Question.Status != "ANSWERED" || answered.Job != nil || answered.Run.Status != "WAITING_FOR_INPUT" {
		t.Fatalf("answered=%+v", answered)
	}
	assertIssueStatus(t, s, f.project.ID, f.issue.ID, "BLOCKED")
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)

	resolved, err := s.ResolveInteractiveQuestion(ctx, f.project.ID, opened.Question.ID)
	if err != nil {
		t.Fatalf("resolve interactive question: %v", err)
	}
	if !resolved.Resumed || resolved.Binding.State != store.InteractiveQuestionResolved || resolved.Run.Status != "RUNNING" {
		t.Fatalf("resolved=%+v", resolved)
	}
	assertIssueStatus(t, s, f.project.ID, f.issue.ID, "IN_PROGRESS")
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)

	replayedResolve, err := s.ResolveInteractiveQuestion(ctx, f.project.ID, opened.Question.ID)
	if err != nil {
		t.Fatalf("re-resolve interactive question: %v", err)
	}
	if replayedResolve.Resumed || replayedResolve.Run.Status != "RUNNING" {
		t.Fatalf("replayed resolve=%+v", replayedResolve)
	}
}

func TestInteractiveQuestionsResumeOnlyAfterAllNativeQuestionsResolve(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "interactive-question-batch")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "interactive-question-batch-start")
	admission := mustAdmit(t, s, "interactive-question-batch-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	open := func(key, prompt string) store.OpenInteractiveQuestionResult {
		t.Helper()
		result, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
			Question: store.Question{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: prompt, Kind: "TEXT", Blocking: true, Status: "OPEN"},
			Engine:   "opencode", CorrelationKey: key,
		})
		if err != nil {
			t.Fatalf("open %s: %v", key, err)
		}
		return result
	}
	first := open("request-2:0", "First?")
	second := open("request-2:1", "Second?")
	if !first.EnteredWaiting || second.EnteredWaiting {
		t.Fatalf("first=%+v second=%+v", first, second)
	}

	for _, question := range []store.Question{first.Question, second.Question} {
		answer := "answer-" + question.ID
		if _, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
			ProjectID: f.project.ID, QuestionID: question.ID,
			Answer: store.QuestionAnswer{Kind: "TEXT", Text: &answer}, ActorType: "HUMAN",
		}); err != nil {
			t.Fatalf("answer %s: %v", question.ID, err)
		}
	}
	firstResolved, err := s.ResolveInteractiveQuestion(ctx, f.project.ID, first.Question.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstResolved.Resumed || firstResolved.Run.Status != "WAITING_FOR_INPUT" {
		t.Fatalf("first resolve=%+v", firstResolved)
	}
	secondResolved, err := s.ResolveInteractiveQuestion(ctx, f.project.ID, second.Question.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !secondResolved.Resumed || secondResolved.Run.Status != "RUNNING" {
		t.Fatalf("second resolve=%+v", secondResolved)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)
}

func TestInteractiveWaitingClaimCanFailWithoutRecoveryReplay(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "interactive-question-fail")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "interactive-question-fail-start")
	admission := mustAdmit(t, s, "interactive-question-fail-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if _, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
		Question: store.Question{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: "Waiting", Kind: "TEXT", Blocking: true, Status: "OPEN"},
		Engine:   "opencode", CorrelationKey: "request-fail:0",
	}); err != nil {
		t.Fatalf("open interactive question: %v", err)
	}

	reason := "native session failed while waiting"
	failed, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "FAILED", FailureReason: &reason,
	})
	if err != nil {
		t.Fatalf("fail live waiting claim: %v", err)
	}
	if failed.Status != "FAILED" {
		t.Fatalf("failed run=%+v", failed)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)
}
