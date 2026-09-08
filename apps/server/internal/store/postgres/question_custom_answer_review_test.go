package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestValidateQuestionAnswerCustomChoiceText(t *testing.T) {
	text := "another answer"
	for _, kind := range []string{"SINGLE_CHOICE", "MULTI_CHOICE"} {
		kind := kind
		t.Run(kind+"/enabled", func(t *testing.T) {
			err := validateQuestionAnswer(store.Question{Kind: kind, Custom: true}, store.QuestionAnswer{
				Kind: kind,
				Text: &text,
			})
			if err != nil {
				t.Fatalf("validateQuestionAnswer() error=%v", err)
			}
		})

		t.Run(kind+"/disabled", func(t *testing.T) {
			err := validateQuestionAnswer(store.Question{Kind: kind}, store.QuestionAnswer{
				Kind: kind,
				Text: &text,
			})
			if !errors.Is(err, store.ErrInvalidArgument) {
				t.Fatalf("validateQuestionAnswer() error=%v want %v", err, store.ErrInvalidArgument)
			}
		})
	}
}

func TestValidateQuestionAnswerRejectsBlankCustomChoiceText(t *testing.T) {
	blank := "  "
	for _, kind := range []string{"SINGLE_CHOICE", "MULTI_CHOICE"} {
		err := validateQuestionAnswer(store.Question{Kind: kind, Custom: true}, store.QuestionAnswer{
			Kind: kind,
			Text: &blank,
		})
		if !errors.Is(err, store.ErrInvalidArgument) {
			t.Fatalf("%s validateQuestionAnswer() error=%v want %v", kind, err, store.ErrInvalidArgument)
		}
	}
}
