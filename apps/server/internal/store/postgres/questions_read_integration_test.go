package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestQuestionReadsAndNonBlockingAnswer(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "question-reads")

	blocking, err := s.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		RunID:     f.run.ID,
		Prompt:    "Need human input",
		Kind:      "TEXT",
		Blocking:  true,
		Status:    "OPEN",
	})
	if err != nil {
		t.Fatalf("create blocking question: %v", err)
	}
	choice, err := s.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID,
		IssueID:   f.issue.ID,
		RunID:     f.run.ID,
		Prompt:    "Pick an option",
		Kind:      "SINGLE_CHOICE",
		Options:   json.RawMessage(`[{"id":"a","label":"A"},{"id":"b","label":"B"}]`),
		Blocking:  false,
		Status:    "OPEN",
	})
	if err != nil {
		t.Fatalf("create choice question: %v", err)
	}

	all, err := s.ListQuestions(ctx, f.project.ID, store.QuestionFilter{})
	if err != nil || len(all) != 2 {
		t.Fatalf("ListQuestions(all)=%+v err=%v", all, err)
	}
	runID := f.run.ID
	open, err := s.ListQuestions(ctx, f.project.ID, store.QuestionFilter{RunID: &runID, Statuses: []string{"OPEN"}})
	if err != nil || len(open) != 2 {
		t.Fatalf("ListQuestions(open)=%+v err=%v", open, err)
	}
	issueID := f.issue.ID
	byIssue, err := s.ListQuestions(ctx, f.project.ID, store.QuestionFilter{IssueID: &issueID})
	if err != nil || len(byIssue) != 2 {
		t.Fatalf("ListQuestions(issue)=%+v err=%v", byIssue, err)
	}

	gotBlocking, err := s.GetOpenBlockingQuestion(ctx, f.project.ID, f.run.ID)
	if err != nil || gotBlocking.ID != blocking.ID {
		t.Fatalf("GetOpenBlockingQuestion()=%+v err=%v", gotBlocking, err)
	}
	if _, err := s.GetQuestion(ctx, f.project.ID, choice.ID); err != nil {
		t.Fatalf("GetQuestion() error=%v", err)
	}

	answered, err := s.AnswerQuestion(ctx, store.AnswerQuestionCommand{
		ProjectID:  f.project.ID,
		QuestionID: choice.ID,
		ActorType:  "HUMAN",
		Answer:     store.QuestionAnswer{Kind: "SINGLE_CHOICE", OptionIDs: []string{"b"}},
	})
	if err != nil {
		t.Fatalf("AnswerQuestion(non-blocking) error=%v", err)
	}
	if answered.Question.Status != "ANSWERED" || answered.Job != nil || answered.Run.ID != f.run.ID {
		t.Fatalf("AnswerQuestion(non-blocking)=%+v", answered)
	}
	decision, err := s.GetDecisionByQuestion(ctx, f.project.ID, choice.ID)
	if err != nil || decision.ID != answered.Decision.ID {
		t.Fatalf("GetDecisionByQuestion()=%+v err=%v", decision, err)
	}
	answeredList, err := s.ListQuestions(ctx, f.project.ID, store.QuestionFilter{Statuses: []string{"ANSWERED"}})
	if err != nil || len(answeredList) != 1 || answeredList[0].ID != choice.ID {
		t.Fatalf("ListQuestions(answered)=%+v err=%v", answeredList, err)
	}
}

func TestValidateQuestionAnswerShapes(t *testing.T) {
	text := "answer"
	blank := "   "
	textQuestion := store.Question{Kind: "TEXT"}
	if err := validateQuestionAnswer(textQuestion, store.QuestionAnswer{Kind: "TEXT", Text: &text}); err != nil {
		t.Fatalf("valid TEXT answer: %v", err)
	}
	for name, answer := range map[string]store.QuestionAnswer{
		"wrong kind": {Kind: "SINGLE_CHOICE", OptionIDs: []string{"a"}},
		"missing text": {Kind: "TEXT"},
		"blank text": {Kind: "TEXT", Text: &blank},
		"text plus option": {Kind: "TEXT", Text: &text, OptionIDs: []string{"a"}},
	} {
		t.Run("text "+name, func(t *testing.T) {
			if err := validateQuestionAnswer(textQuestion, answer); !errors.Is(err, store.ErrInvalidArgument) {
				t.Fatalf("error=%v want invalid argument", err)
			}
		})
	}

	options := json.RawMessage(`[{"id":"a","label":"A"},{"id":"b","label":"B"}]`)
	single := store.Question{Kind: "SINGLE_CHOICE", Options: options}
	if err := validateQuestionAnswer(single, store.QuestionAnswer{Kind: "SINGLE_CHOICE", OptionIDs: []string{"a"}}); err != nil {
		t.Fatalf("valid SINGLE_CHOICE answer: %v", err)
	}
	for name, answer := range map[string]store.QuestionAnswer{
		"missing option": {Kind: "SINGLE_CHOICE"},
		"multiple options": {Kind: "SINGLE_CHOICE", OptionIDs: []string{"a", "b"}},
		"unknown option": {Kind: "SINGLE_CHOICE", OptionIDs: []string{"missing"}},
		"text included": {Kind: "SINGLE_CHOICE", Text: &text, OptionIDs: []string{"a"}},
	} {
		t.Run("single "+name, func(t *testing.T) {
			if err := validateQuestionAnswer(single, answer); !errors.Is(err, store.ErrInvalidArgument) {
				t.Fatalf("error=%v want invalid argument", err)
			}
		})
	}

	multi := store.Question{Kind: "MULTI_CHOICE", Options: options}
	if err := validateQuestionAnswer(multi, store.QuestionAnswer{Kind: "MULTI_CHOICE", OptionIDs: []string{"a", "b"}}); err != nil {
		t.Fatalf("valid MULTI_CHOICE answer: %v", err)
	}
	if err := validateQuestionAnswer(multi, store.QuestionAnswer{Kind: "MULTI_CHOICE", OptionIDs: []string{"a", "a"}}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("duplicate MULTI_CHOICE error=%v", err)
	}
	invalidOptions := store.Question{Kind: "SINGLE_CHOICE", Options: json.RawMessage(`{}`)}
	if err := validateQuestionAnswer(invalidOptions, store.QuestionAnswer{Kind: "SINGLE_CHOICE", OptionIDs: []string{"a"}}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid persisted options error=%v", err)
	}
	unsupported := store.Question{Kind: "UNKNOWN"}
	if err := validateQuestionAnswer(unsupported, store.QuestionAnswer{Kind: "UNKNOWN"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("unsupported kind error=%v", err)
	}

	if got := questionAnswerOutcome(store.QuestionAnswer{Kind: "TEXT", Text: &text}); got != text {
		t.Fatalf("text outcome=%q", got)
	}
	if got := questionAnswerOutcome(store.QuestionAnswer{Kind: "MULTI_CHOICE", OptionIDs: []string{"a", "b"}}); got != `["a","b"]` {
		t.Fatalf("choice outcome=%q", got)
	}
}
