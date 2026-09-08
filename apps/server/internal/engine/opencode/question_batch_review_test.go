package opencode

import (
	"context"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func (q *fakeInteractiveQuestions) OpenBatch(_ context.Context, requests []engine.CorrelatedQuestionRequest) ([]engine.Question, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	opened := make([]engine.Question, len(requests))
	if q.answers == nil {
		q.answers = make(map[string]engine.QuestionAnswer)
	}
	for index, request := range requests {
		id := "canonical-" + strings.ReplaceAll(request.CorrelationKey, "/", "-")
		q.opened = append(q.opened, id)
		q.correlations = append(q.correlations, request.CorrelationKey)
		if request.Question.Kind == "TEXT" {
			text := "because it is safer"
			q.answers[id] = engine.QuestionAnswer{Kind: "TEXT", Text: &text}
		} else {
			q.answers[id] = engine.QuestionAnswer{Kind: request.Question.Kind, OptionIDs: []string{"option-1"}}
		}
		opened[index] = engine.Question{ID: id, Blocking: true}
	}
	return opened, nil
}

func TestHandleQuestionMapsEntireBatchBeforeOpening(t *testing.T) {
	questions := &fakeInteractiveQuestions{}
	state := newRunState("ses_atomic", questions, nil)
	err := state.handleQuestion(context.Background(), nil, client.QuestionRequest{
		ID:        "que_atomic",
		SessionID: "ses_atomic",
		Questions: []client.QuestionInfo{
			{Question: "Valid first Question"},
			{Question: "   "},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "map native Question que_atomic[1]") {
		t.Fatalf("handleQuestion() error=%v", err)
	}
	questions.mu.Lock()
	defer questions.mu.Unlock()
	if len(questions.opened) != 0 || len(questions.correlations) != 0 {
		t.Fatalf("partial batch persisted in fake capability: opened=%v correlations=%v", questions.opened, questions.correlations)
	}
}

var _ engine.InteractiveQuestionBatcher = (*fakeInteractiveQuestions)(nil)
