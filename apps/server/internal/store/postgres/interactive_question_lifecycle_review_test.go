package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestInteractiveQuestionAnswerRequeuesAfterLiveClaimExpires(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "interactive-answer-expired-claim")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "interactive-answer-expired-claim-start")
	admission := mustAdmit(t, s, "interactive-answer-expired-claim-worker")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	opened, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
		Question: store.Question{
			ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID,
			Prompt: "Continue after lease loss?", Kind: "TEXT", Blocking: true, Status: "OPEN",
		},
		Engine: "opencode", CorrelationKey: "session-expired/request/0",
	})
	if err != nil {
		t.Fatalf("open interactive Question: %v", err)
	}
	expireLease(t, s, admission.Job.ID)

	answer := "continue"
	answered, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID: f.project.ID, QuestionID: opened.Question.ID,
		Answer: store.QuestionAnswer{Kind: "TEXT", Text: &answer}, ActorType: "HUMAN",
	})
	if err != nil {
		t.Fatalf("answer after claim expiry: %v", err)
	}
	if answered.Question.Status != "ANSWERED" || answered.Run.Status != "QUEUED" || answered.Job == nil || answered.Job.Kind != "RESUME" || answered.Job.State != "QUEUED" {
		t.Fatalf("answer result=%+v", answered)
	}
	assertIssueStatus(t, s, f.project.ID, f.issue.ID, "IN_PROGRESS")
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)

	var oldJobState, bindingState string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE project_id=$1 AND id=$2`, f.project.ID, admission.Job.ID).Scan(&oldJobState); err != nil {
		t.Fatalf("read expired job state: %v", err)
	}
	if oldJobState != "DONE" {
		t.Fatalf("expired job state=%s want DONE", oldJobState)
	}
	if err := s.pool.QueryRow(ctx, `SELECT state FROM engine_question_bindings WHERE project_id=$1 AND question_id=$2`, f.project.ID, opened.Question.ID).Scan(&bindingState); err != nil {
		t.Fatalf("read binding state: %v", err)
	}
	if bindingState != store.InteractiveQuestionAnswered {
		t.Fatalf("binding state=%s want %s", bindingState, store.InteractiveQuestionAnswered)
	}

	resume := mustAdmit(t, s, "interactive-answer-expired-resume-worker")
	if resume.Job.ID != answered.Job.ID || resume.Job.Kind != "RESUME" || resume.Run.ID != f.run.ID {
		t.Fatalf("resume admission=%+v", resume)
	}
}

func TestTerminalLiveWaitingRunCleansInteractiveQuestionState(t *testing.T) {
	tests := []struct {
		name           string
		runStatus      string
		answerFirst    bool
		questionStatus string
	}{
		{name: "failed with open Question", runStatus: "FAILED", questionStatus: "CANCELLED"},
		{name: "cancelled after answer", runStatus: "CANCELLED", answerFirst: true, questionStatus: "ANSWERED"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			s := New(testPool(t))
			ctx := context.Background()
			f := seedRunFixture(t, s, "terminal-live-question-"+testCase.runStatus)
			f.issue.Status = "IN_PROGRESS"
			if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
				t.Fatalf("set issue in progress: %v", err)
			}
			enqueueFixtureRun(t, s, f, f.run, "terminal-live-question-start-"+testCase.runStatus)
			admission := mustAdmit(t, s, "terminal-live-question-worker-"+testCase.runStatus)
			if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
				ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
				LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
			}); err != nil {
				t.Fatalf("mark running: %v", err)
			}
			opened, err := s.OpenInteractiveQuestion(ctx, store.OpenInteractiveQuestionCommand{
				Question: store.Question{
					ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID,
					Prompt: "Pending native input", Kind: "TEXT", Blocking: true, Status: "OPEN",
				},
				Engine: "opencode", CorrelationKey: "terminal/" + testCase.runStatus + "/0",
			})
			if err != nil {
				t.Fatalf("open interactive Question: %v", err)
			}
			if testCase.answerFirst {
				answer := "answer persisted before native completion"
				answered, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
					ProjectID: f.project.ID, QuestionID: opened.Question.ID,
					Answer: store.QuestionAnswer{Kind: "TEXT", Text: &answer}, ActorType: "HUMAN",
				})
				if err != nil {
					t.Fatalf("answer interactive Question: %v", err)
				}
				if answered.Job != nil || answered.Run.Status != "WAITING_FOR_INPUT" {
					t.Fatalf("live answer result=%+v", answered)
				}
			}

			transition := store.SchedulerTransition{
				ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
				LeaseToken: admission.Lease.LeaseToken, RunStatus: testCase.runStatus,
			}
			if testCase.runStatus == "FAILED" {
				reason := "native session failed"
				transition.FailureReason = &reason
			}
			run, err := s.TransitionAdmittedJob(ctx, transition)
			if err != nil {
				t.Fatalf("terminal transition: %v", err)
			}
			if run.Status != testCase.runStatus {
				t.Fatalf("run status=%s want %s", run.Status, testCase.runStatus)
			}

			question, err := s.GetQuestion(ctx, f.project.ID, opened.Question.ID)
			if err != nil {
				t.Fatalf("read Question: %v", err)
			}
			if question.Status != testCase.questionStatus {
				t.Fatalf("question status=%s want %s", question.Status, testCase.questionStatus)
			}
			var bindingState string
			if err := s.pool.QueryRow(ctx, `SELECT state FROM engine_question_bindings WHERE project_id=$1 AND question_id=$2`, f.project.ID, opened.Question.ID).Scan(&bindingState); err != nil {
				t.Fatalf("read binding state: %v", err)
			}
			if bindingState != store.InteractiveQuestionCancelled {
				t.Fatalf("binding state=%s want %s", bindingState, store.InteractiveQuestionCancelled)
			}
			assertIssueStatus(t, s, f.project.ID, f.issue.ID, "IN_PROGRESS")
			assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 0, 0)
		})
	}
}
