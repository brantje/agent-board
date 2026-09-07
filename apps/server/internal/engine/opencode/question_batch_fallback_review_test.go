package opencode

import (
	"context"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

type singleInteractiveQuestioner struct {
	correlation string
	request     engine.QuestionRequest
}

func (q *singleInteractiveQuestioner) Open(_ context.Context, correlation string, request engine.QuestionRequest) (engine.Question, error) {
	q.correlation = correlation
	q.request = request
	return engine.Question{ID: "question-single", Blocking: true}, nil
}

func (*singleInteractiveQuestioner) WaitAnswer(context.Context, string) (engine.QuestionAnswer, error) {
	return engine.QuestionAnswer{}, nil
}

func (*singleInteractiveQuestioner) Resolve(context.Context, string) error { return nil }

func TestOpenQuestionBatchFallsBackForSingleQuestion(t *testing.T) {
	questions := &singleInteractiveQuestioner{}
	state := newRunState("ses-single", questions, nil)
	opened, err := state.openQuestionBatch(context.Background(), []engine.CorrelatedQuestionRequest{{
		CorrelationKey: "ses-single/request/0",
		Question:       engine.QuestionRequest{Prompt: "Why?", Kind: "TEXT", Blocking: true},
	}})
	if err != nil {
		t.Fatalf("openQuestionBatch() error=%v", err)
	}
	if len(opened) != 1 || opened[0].ID != "question-single" {
		t.Fatalf("opened=%+v", opened)
	}
	if questions.correlation != "ses-single/request/0" || questions.request.Prompt != "Why?" {
		t.Fatalf("fallback request correlation=%q request=%+v", questions.correlation, questions.request)
	}
}

func TestOpenQuestionBatchRequiresBatchCapabilityForMultipleQuestions(t *testing.T) {
	state := newRunState("ses-multi", &singleInteractiveQuestioner{}, nil)
	_, err := state.openQuestionBatch(context.Background(), []engine.CorrelatedQuestionRequest{
		{CorrelationKey: "ses-multi/request/0", Question: engine.QuestionRequest{Prompt: "First?", Kind: "TEXT", Blocking: true}},
		{CorrelationKey: "ses-multi/request/1", Question: engine.QuestionRequest{Prompt: "Second?", Kind: "TEXT", Blocking: true}},
	})
	if err == nil || !strings.Contains(err.Error(), "batch capability is required") {
		t.Fatalf("openQuestionBatch() error=%v", err)
	}
}

var _ engine.InteractiveQuestioner = (*singleInteractiveQuestioner)(nil)
