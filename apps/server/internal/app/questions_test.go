package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type questionServiceStore struct {
	questions   []store.Question
	listErr     error
	getErr      error
	answerErr   error
	lastFilter  store.QuestionFilter
	lastCommand store.AnswerQuestionCommand
}

func (s *questionServiceStore) CreateQuestion(context.Context, store.Question) (store.Question, error) {
	return store.Question{}, nil
}

func (s *questionServiceStore) GetQuestion(_ context.Context, projectID, questionID string) (store.Question, error) {
	if s.getErr != nil {
		return store.Question{}, s.getErr
	}
	for _, question := range s.questions {
		if question.ProjectID == projectID && question.ID == questionID {
			return question, nil
		}
	}
	return store.Question{}, store.ErrNotFound
}

func (s *questionServiceStore) ListQuestions(_ context.Context, _ string, filter store.QuestionFilter) ([]store.Question, error) {
	s.lastFilter = filter
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]store.Question(nil), s.questions...), nil
}

func (s *questionServiceStore) GetDecisionByQuestion(context.Context, string, string) (store.Decision, error) {
	return store.Decision{}, store.ErrNotFound
}

func (s *questionServiceStore) GetOpenBlockingQuestion(context.Context, string, string) (store.Question, error) {
	return store.Question{}, store.ErrNotFound
}

func (s *questionServiceStore) AnswerQuestion(_ context.Context, command store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	s.lastCommand = command
	if s.answerErr != nil {
		return store.AnswerQuestionResult{}, s.answerErr
	}
	return store.AnswerQuestionResult{Question: store.Question{ID: command.QuestionID, ProjectID: command.ProjectID, Status: "ANSWERED"}}, nil
}

func TestQuestionServiceRoutesStoreOperations(t *testing.T) {
	projectID := "project-1"
	questionID := "question-1"
	issueID := "issue-1"
	runID := "run-1"
	questionStore := &questionServiceStore{questions: []store.Question{{ID: questionID, ProjectID: projectID, IssueID: issueID, RunID: runID, Prompt: "Continue?", Kind: "TEXT", Status: "OPEN"}}}
	service, err := NewQuestionService(questionStore)
	if err != nil {
		t.Fatal(err)
	}

	filter := store.QuestionFilter{RunID: &runID, Statuses: []string{"OPEN"}}
	questions, err := service.List(context.Background(), projectID, filter)
	if err != nil || len(questions) != 1 || questions[0].ID != questionID {
		t.Fatalf("List() questions=%+v err=%v", questions, err)
	}
	if questionStore.lastFilter.RunID == nil || *questionStore.lastFilter.RunID != runID || len(questionStore.lastFilter.Statuses) != 1 {
		t.Fatalf("List() filter=%+v", questionStore.lastFilter)
	}

	question, err := service.Get(context.Background(), projectID, questionID)
	if err != nil || question.ID != questionID {
		t.Fatalf("Get() question=%+v err=%v", question, err)
	}

	actorID := "human-1"
	answer := "Use the safe path"
	result, err := service.Answer(context.Background(), projectID, questionID, store.QuestionAnswer{Kind: "TEXT", Text: &answer}, &actorID)
	if err != nil || result.Question.Status != "ANSWERED" {
		t.Fatalf("Answer() result=%+v err=%v", result, err)
	}
	if questionStore.lastCommand.ActorType != "HUMAN" || questionStore.lastCommand.ActorID == nil || *questionStore.lastCommand.ActorID != actorID {
		t.Fatalf("Answer() command=%+v", questionStore.lastCommand)
	}
}

func TestQuestionServiceValidatesAndTranslatesErrors(t *testing.T) {
	if _, err := NewQuestionService(nil); err == nil {
		t.Fatal("expected nil store rejection")
	}

	service, err := NewQuestionService(&questionServiceStore{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(context.Background(), " ", store.QuestionFilter{}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("List() error=%v want invalid argument", err)
	}
	if _, err := service.Get(context.Background(), "project-1", " "); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("Get() error=%v want invalid argument", err)
	}
	if _, err := service.Answer(context.Background(), "", "question-1", store.QuestionAnswer{}, nil); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("Answer() error=%v want invalid argument", err)
	}

	missing, _ := NewQuestionService(&questionServiceStore{getErr: store.ErrNotFound})
	if _, err := missing.Get(context.Background(), "project-1", "question-1"); err == nil {
		t.Fatal("expected translated not-found error")
	} else if appErr, ok := AsError(err); !ok || appErr.Code != "question_not_found" {
		t.Fatalf("Get() translated error=%v", err)
	}

	conflicting, _ := NewQuestionService(&questionServiceStore{answerErr: store.ErrConflict})
	if _, err := conflicting.Answer(context.Background(), "project-1", "question-1", store.QuestionAnswer{}, nil); err == nil {
		t.Fatal("expected translated conflict")
	} else if appErr, ok := AsError(err); !ok || appErr.Code != "conflict" {
		t.Fatalf("Answer() translated error=%v", err)
	}

	listing, _ := NewQuestionService(&questionServiceStore{listErr: store.ErrInvalidArgument})
	if _, err := listing.List(context.Background(), "project-1", store.QuestionFilter{}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("List() translated error=%v", err)
	}
}
