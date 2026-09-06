package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestBlockingQuestionAnswerRequiresWaitingRun(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "question-answer-requires-waiting")
	question, err := s.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		RunID:     f.run.ID,
		Prompt:    "Need input",
		Kind:      "TEXT",
		Blocking:  true,
		Status:    "OPEN",
	})
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	answer := "continue"
	if _, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID:  f.project.ID,
		QuestionID: question.ID,
		Answer:     store.QuestionAnswer{Kind: "TEXT", Text: &answer},
		ActorType:  "HUMAN",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("answer error=%v want conflict", err)
	}
	persisted, err := s.GetQuestion(ctx, f.project.ID, question.ID)
	if err != nil {
		t.Fatalf("read question: %v", err)
	}
	if persisted.Status != "OPEN" || persisted.AnsweredAt != nil {
		t.Fatalf("question changed after rejected answer: %+v", persisted)
	}
}

func TestBlockingQuestionAnswerRejectsActiveSchedulerWork(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "question-answer-active-job")
	enqueueFixtureRun(t, s, f, f.run, "question-answer-active-job")
	admission := mustAdmit(t, s, "worker-question-answer-active-job")

	question, err := s.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		RunID:     f.run.ID,
		Prompt:    "Need input",
		Kind:      "TEXT",
		Blocking:  true,
		Status:    "OPEN",
	})
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE runs SET status='WAITING_FOR_INPUT' WHERE project_id=$1 AND id=$2;
		UPDATE issues SET status='BLOCKED' WHERE project_id=$1 AND id=$3;
	`, f.project.ID, f.run.ID, f.issue.ID); err != nil {
		t.Fatalf("prepare waiting state: %v", err)
	}

	answer := "continue"
	if _, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID:  f.project.ID,
		QuestionID: question.ID,
		Answer:     store.QuestionAnswer{Kind: "TEXT", Text: &answer},
		ActorType:  "HUMAN",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("answer error=%v want conflict", err)
	}
	persisted, err := s.GetQuestion(ctx, f.project.ID, question.ID)
	if err != nil {
		t.Fatalf("read question: %v", err)
	}
	if persisted.Status != "OPEN" {
		t.Fatalf("question status=%s want OPEN", persisted.Status)
	}
	assertSchedulerOwnershipCounts(t, s, admission.Job.ID, 1, 2)
}

func TestAnswerQuestionRejectsInvalidCommand(t *testing.T) {
	s := New(testPool(t))
	if _, err := s.AnswerQuestion(context.Background(), store.AnswerQuestionCommand{}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("empty command error=%v want invalid argument", err)
	}
	if _, err := s.AnswerQuestion(context.Background(), store.AnswerQuestionCommand{
		ProjectID:  "project",
		QuestionID: "question",
		ActorType:  "AGENT",
	}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("non-human actor error=%v want invalid argument", err)
	}
}
