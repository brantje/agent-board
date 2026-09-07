package evidence

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type questionRedactionStore struct {
	store.ControlPlaneStore
	question       store.Question
	decision       store.Decision
	created        store.Question
	answered       store.AnswerQuestionCommand
	listFilter     store.QuestionFilter
	openRunID      string
	decisionLookup string
}

func (s *questionRedactionStore) CreateQuestion(_ context.Context, input store.Question) (store.Question, error) {
	s.created = input
	input.ID = "question-created"
	return input, nil
}

func (s *questionRedactionStore) GetQuestion(context.Context, string, string) (store.Question, error) {
	return s.question, nil
}

func (s *questionRedactionStore) ListQuestions(_ context.Context, _ string, filter store.QuestionFilter) ([]store.Question, error) {
	s.listFilter = filter
	return []store.Question{s.question}, nil
}

func (s *questionRedactionStore) GetDecisionByQuestion(_ context.Context, _, questionID string) (store.Decision, error) {
	s.decisionLookup = questionID
	return s.decision, nil
}

func (s *questionRedactionStore) GetOpenBlockingQuestion(_ context.Context, _, runID string) (store.Question, error) {
	s.openRunID = runID
	return s.question, nil
}

func (s *questionRedactionStore) AnswerQuestion(_ context.Context, input store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	s.answered = input
	return store.AnswerQuestionResult{Question: s.question, Decision: s.decision}, nil
}

type noQuestionControlPlane struct{ store.ControlPlaneStore }

func TestRedactingStoreQuestionOperations(t *testing.T) {
	const runID = "run-1"
	registry := redaction.NewRegistry()
	registry.Register(runID, []string{"secret"})

	base := &questionRedactionStore{
		question: store.Question{ID: "question-1", ProjectID: "project-1", IssueID: "issue-1", RunID: runID, Prompt: "prompt", Kind: "TEXT", Status: "OPEN"},
		decision: store.Decision{ID: "decision-1", ProjectID: "project-1"},
	}
	secured := NewRedactingStore(base, registry)

	recommendation := "prefer secret path"
	created, err := secured.CreateQuestion(context.Background(), store.Question{
		ProjectID:      "project-1",
		IssueID:        "issue-1",
		RunID:          runID,
		Prompt:         "do not reveal secret",
		Kind:           "SINGLE_CHOICE",
		Options:        json.RawMessage(`[{"id":"safe","label":"secret choice"}]`),
		Recommendation: &recommendation,
		Blocking:       true,
	})
	if err != nil || created.ID != "question-created" {
		t.Fatalf("CreateQuestion()=%+v err=%v", created, err)
	}
	if strings.Contains(base.created.Prompt, "secret") || base.created.Recommendation == nil || strings.Contains(*base.created.Recommendation, "secret") || strings.Contains(string(base.created.Options), "secret") {
		t.Fatalf("CreateQuestion() leaked secret: %+v options=%s", base.created, base.created.Options)
	}
	if !strings.Contains(base.created.Prompt, "***") || !strings.Contains(string(base.created.Options), "***") {
		t.Fatalf("CreateQuestion() did not redact values: %+v options=%s", base.created, base.created.Options)
	}

	if question, err := secured.GetQuestion(context.Background(), "project-1", "question-1"); err != nil || question.ID != "question-1" {
		t.Fatalf("GetQuestion()=%+v err=%v", question, err)
	}
	runFilter := runID
	if questions, err := secured.ListQuestions(context.Background(), "project-1", store.QuestionFilter{RunID: &runFilter, Statuses: []string{"OPEN"}}); err != nil || len(questions) != 1 {
		t.Fatalf("ListQuestions()=%+v err=%v", questions, err)
	}
	if base.listFilter.RunID == nil || *base.listFilter.RunID != runID {
		t.Fatalf("ListQuestions() filter=%+v", base.listFilter)
	}
	if decision, err := secured.GetDecisionByQuestion(context.Background(), "project-1", "question-1"); err != nil || decision.ID != "decision-1" || base.decisionLookup != "question-1" {
		t.Fatalf("GetDecisionByQuestion()=%+v lookup=%q err=%v", decision, base.decisionLookup, err)
	}
	if question, err := secured.GetOpenBlockingQuestion(context.Background(), "project-1", runID); err != nil || question.ID != "question-1" || base.openRunID != runID {
		t.Fatalf("GetOpenBlockingQuestion()=%+v run=%q err=%v", question, base.openRunID, err)
	}

	text := "answer contains secret"
	if _, err := secured.AnswerQuestion(context.Background(), store.AnswerQuestionCommand{
		ProjectID: "project-1", QuestionID: "question-1", ActorType: "HUMAN",
		Answer: store.QuestionAnswer{Kind: "TEXT", Text: &text},
	}); err != nil {
		t.Fatalf("AnswerQuestion(TEXT) error=%v", err)
	}
	if base.answered.Answer.Text == nil || strings.Contains(*base.answered.Answer.Text, "secret") || !strings.Contains(*base.answered.Answer.Text, "***") {
		t.Fatalf("AnswerQuestion(TEXT)=%+v", base.answered.Answer)
	}

	base.question.Kind = "SINGLE_CHOICE"
	if _, err := secured.AnswerQuestion(context.Background(), store.AnswerQuestionCommand{
		ProjectID: "project-1", QuestionID: "question-1", ActorType: "HUMAN",
		Answer: store.QuestionAnswer{Kind: "SINGLE_CHOICE", OptionIDs: []string{"secret-option"}},
	}); err != nil {
		t.Fatalf("AnswerQuestion(choice) error=%v", err)
	}
	if len(base.answered.Answer.OptionIDs) != 1 || strings.Contains(base.answered.Answer.OptionIDs[0], "secret") || !strings.Contains(base.answered.Answer.OptionIDs[0], "***") {
		t.Fatalf("AnswerQuestion(choice)=%+v", base.answered.Answer)
	}
}

func TestRedactingStoreRejectsMissingQuestionCapability(t *testing.T) {
	secured := NewRedactingStore(&noQuestionControlPlane{}, redaction.NewRegistry())
	if _, err := secured.GetQuestion(context.Background(), "project-1", "question-1"); err == nil || !strings.Contains(err.Error(), "does not support Question operations") {
		t.Fatalf("GetQuestion() error=%v", err)
	}
}
