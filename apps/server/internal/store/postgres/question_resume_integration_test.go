package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestBlockingQuestionAnswerDurablyResumesSameRunAndWorkspace(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "question-resume-lifecycle")
	f.issue.Status = "IN_PROGRESS"
	if _, err := s.UpdateIssue(ctx, f.issue); err != nil {
		t.Fatalf("set issue in progress: %v", err)
	}
	enqueueFixtureRun(t, s, f, f.run, "question-resume-start")
	start := mustAdmit(t, s, "worker-start")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      start.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: start.Lease.LeaseToken,
		RunStatus:  "RUNNING",
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	question, err := s.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		RunID:     f.run.ID,
		Prompt:    "Which path should the agent take?",
		Kind:      "TEXT",
		Blocking:  true,
		Status:    "OPEN",
	})
	if err != nil {
		t.Fatalf("create blocking question: %v", err)
	}
	waiting, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      start.Job.ID,
		RunID:      f.run.ID,
		LeaseToken: start.Lease.LeaseToken,
		RunStatus:  "WAITING_FOR_INPUT",
	})
	if err != nil {
		t.Fatalf("mark waiting: %v", err)
	}
	if waiting.ID != f.run.ID || waiting.WorkspaceID != f.workspace.ID || waiting.Status != "WAITING_FOR_INPUT" {
		t.Fatalf("waiting run=%+v", waiting)
	}
	issue, err := s.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil || issue.Status != "BLOCKED" {
		t.Fatalf("blocked issue=%+v err=%v", issue, err)
	}
	assertSchedulerOwnershipCounts(t, s, start.Job.ID, 0, 0)

	answer := "Use the durable workspace"
	answered, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID:  f.project.ID,
		QuestionID: question.ID,
		Answer:     store.QuestionAnswer{Kind: "TEXT", Text: &answer},
		ActorType:  "HUMAN",
	})
	if err != nil {
		t.Fatalf("answer question: %v", err)
	}
	if answered.Question.Status != "ANSWERED" || answered.Question.AnsweredAt == nil {
		t.Fatalf("answered question=%+v", answered.Question)
	}
	if answered.Decision.QuestionID == nil || *answered.Decision.QuestionID != question.ID {
		t.Fatalf("decision=%+v", answered.Decision)
	}
	if answered.Run.ID != f.run.ID || answered.Run.WorkspaceID != f.workspace.ID || answered.Run.Status != "QUEUED" {
		t.Fatalf("resumed run=%+v", answered.Run)
	}
	if answered.Job == nil || answered.Job.Kind != "RESUME" || answered.Job.RunID != f.run.ID || answered.Job.State != "QUEUED" {
		t.Fatalf("resume job=%+v", answered.Job)
	}
	issue, err = s.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil || issue.Status != "IN_PROGRESS" {
		t.Fatalf("resumed issue=%+v err=%v", issue, err)
	}
	if _, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID:  f.project.ID,
		QuestionID: question.ID,
		Answer:     store.QuestionAnswer{Kind: "TEXT", Text: &answer},
		ActorType:  "HUMAN",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate answer error=%v want conflict", err)
	}

	resume, err := s.AdmitNextJob(ctx, "worker-resume", time.Minute, time.Second)
	if err != nil {
		t.Fatalf("admit resume: %v", err)
	}
	if resume == nil || resume.Job.ID != answered.Job.ID || resume.Job.Kind != "RESUME" {
		t.Fatalf("resume admission=%+v", resume)
	}
	if resume.Run.ID != f.run.ID || resume.Run.WorkspaceID != f.workspace.ID || resume.Run.Status != "STARTING" {
		t.Fatalf("resume run identity changed: %+v", resume.Run)
	}
}
