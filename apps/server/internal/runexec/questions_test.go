package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type questionTestStore struct {
	created []store.Question
	open    *store.Question
}

func (s *questionTestStore) CreateQuestion(_ context.Context, question store.Question) (store.Question, error) {
	question.ID = "question-1"
	s.created = append(s.created, question)
	if question.Blocking && question.Status == "OPEN" {
		copy := question
		s.open = &copy
	}
	return question, nil
}

func (s *questionTestStore) GetQuestion(context.Context, string, string) (store.Question, error) {
	return store.Question{}, store.ErrNotFound
}
func (s *questionTestStore) ListQuestions(context.Context, string, store.QuestionFilter) ([]store.Question, error) {
	return nil, nil
}
func (s *questionTestStore) GetDecisionByQuestion(context.Context, string, string) (store.Decision, error) {
	return store.Decision{}, store.ErrNotFound
}
func (s *questionTestStore) GetOpenBlockingQuestion(context.Context, string, string) (store.Question, error) {
	if s.open == nil {
		return store.Question{}, store.ErrNotFound
	}
	return *s.open, nil
}
func (s *questionTestStore) AnswerQuestion(context.Context, store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	return store.AnswerQuestionResult{}, nil
}

type questionEventStore struct{ events []store.Event }

func (s *questionEventStore) AppendEvent(_ context.Context, event store.Event) (store.Event, error) {
	s.events = append(s.events, event)
	return event, nil
}

func TestQuestionerPersistsBlockingQuestionBeforeWaiting(t *testing.T) {
	questions := &questionTestStore{}
	events := &questionEventStore{}
	recorder, err := evidence.NewRecorder(events, nil)
	if err != nil {
		t.Fatal(err)
	}
	q := &questioner{
		store:  questions,
		events: recorder,
		safe: executioncontext.SafeContext{
			Project:   executioncontext.ProjectContext{ID: "project-1"},
			Issue:     executioncontext.IssueContext{ID: "issue-1"},
			Run:       executioncontext.RunContext{ID: "run-1"},
			Agent:     executioncontext.AgentContext{ID: "agent-1"},
			Workspace: executioncontext.WorkspaceContext{ID: "workspace-1"},
		},
		runtimeInstanceID: "runtime-instance-1",
	}
	request := engine.QuestionRequest{Prompt: "Which strategy?", Kind: "SINGLE_CHOICE", Blocking: true, Options: []engine.QuestionOption{{ID: "safe", Label: "Safe"}, {ID: "fast", Label: "Fast"}}}

	created, err := q.Ask(context.Background(), request)
	if !errors.Is(err, engine.ErrWaitingForInput) {
		t.Fatalf("Ask error = %v, want ErrWaitingForInput", err)
	}
	if created.ID != "question-1" || len(questions.created) != 1 {
		t.Fatalf("created=%+v count=%d", created, len(questions.created))
	}
	if len(events.events) != 1 || events.events[0].Type != "question.created" {
		t.Fatalf("events=%+v", events.events)
	}

	retried, err := q.Ask(context.Background(), request)
	if !errors.Is(err, engine.ErrWaitingForInput) {
		t.Fatalf("retry error = %v, want ErrWaitingForInput", err)
	}
	if retried.ID != created.ID || len(questions.created) != 1 {
		t.Fatalf("retry=%+v created count=%d", retried, len(questions.created))
	}
}

func TestValidateQuestionRequest(t *testing.T) {
	tests := []struct {
		name    string
		request engine.QuestionRequest
		wantErr bool
	}{
		{name: "text", request: engine.QuestionRequest{Prompt: "Explain", Kind: "TEXT"}},
		{name: "blank prompt", request: engine.QuestionRequest{Prompt: " ", Kind: "TEXT"}, wantErr: true},
		{name: "text options", request: engine.QuestionRequest{Prompt: "Explain", Kind: "TEXT", Options: []engine.QuestionOption{{ID: "a", Label: "A"}}}, wantErr: true},
		{name: "choice requires options", request: engine.QuestionRequest{Prompt: "Choose", Kind: "SINGLE_CHOICE"}, wantErr: true},
		{name: "duplicate option", request: engine.QuestionRequest{Prompt: "Choose", Kind: "MULTI_CHOICE", Options: []engine.QuestionOption{{ID: "a", Label: "A"}, {ID: "a", Label: "Again"}}}, wantErr: true},
		{name: "valid choice", request: engine.QuestionRequest{Prompt: "Choose", Kind: "MULTI_CHOICE", Options: []engine.QuestionOption{{ID: "a", Label: "A"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQuestionRequest(tt.request)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
