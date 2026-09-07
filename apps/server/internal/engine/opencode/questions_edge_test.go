package opencode

import (
	"context"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestMapNativeQuestionRejectsInvalidPromptsAndOptions(t *testing.T) {
	if _, _, err := mapNativeQuestion(client.QuestionInfo{}); err == nil || !strings.Contains(err.Error(), "prompt is empty") {
		t.Fatalf("empty prompt error=%v", err)
	}
	if _, _, err := mapNativeQuestion(client.QuestionInfo{
		Question: "Choose",
		Options:  []client.QuestionOption{{Label: " "}},
	}); err == nil || !strings.Contains(err.Error(), "has no label") {
		t.Fatalf("empty option label error=%v", err)
	}
}

func TestMapNativeQuestionKeepsMixedCustomChoiceCanonical(t *testing.T) {
	custom := true
	mapped, labels, err := mapNativeQuestion(client.QuestionInfo{
		Question: "Choose or enter custom text",
		Custom:   &custom,
		Options: []client.QuestionOption{
			{Label: "Alpha"},
			{Label: "Beta"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Kind != "SINGLE_CHOICE" || len(mapped.Options) != 2 || labels["option-1"] != "Beta" {
		t.Fatalf("mixed custom Question mapped=%+v labels=%v", mapped, labels)
	}
}

func TestMapCanonicalAnswerRejectsEmptyAndUnsupportedAnswers(t *testing.T) {
	empty := " "
	for _, testCase := range []struct {
		name    string
		binding nativeQuestionBinding
		answer  engine.QuestionAnswer
	}{
		{
			name:    "empty text",
			binding: nativeQuestionBinding{kind: "TEXT"},
			answer:  engine.QuestionAnswer{Kind: "TEXT", Text: &empty},
		},
		{
			name:    "missing text",
			binding: nativeQuestionBinding{kind: "TEXT"},
			answer:  engine.QuestionAnswer{Kind: "TEXT"},
		},
		{
			name:    "empty choice",
			binding: nativeQuestionBinding{kind: "MULTI_CHOICE", labels: map[string]string{"option-0": "Alpha"}},
			answer:  engine.QuestionAnswer{Kind: "MULTI_CHOICE"},
		},
		{
			name:    "unsupported kind",
			binding: nativeQuestionBinding{kind: "FUTURE"},
			answer:  engine.QuestionAnswer{Kind: "FUTURE"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := mapCanonicalAnswer(testCase.binding, testCase.answer); err == nil {
				t.Fatalf("answer unexpectedly accepted: %+v", testCase.answer)
			}
		})
	}
}

func TestRunStatePendingQuestionValidationAndShapeReconciliation(t *testing.T) {
	state := newRunState("ses_1", &fakeInteractiveQuestions{}, nil)
	if err := state.handlePending(context.Background(), nil, []client.QuestionRequest{{
		ID: "wrong", SessionID: "other", Questions: []client.QuestionInfo{{Question: "Ignored"}},
	}}); err != nil {
		t.Fatalf("wrong-session pending request error=%v", err)
	}
	if len(state.nativeQuestions) != 0 {
		t.Fatalf("wrong-session request changed state: %+v", state.nativeQuestions)
	}

	for _, request := range []client.QuestionRequest{
		{SessionID: "ses_1", Questions: []client.QuestionInfo{{Question: "Missing ID"}}},
		{ID: "empty", SessionID: "ses_1"},
	} {
		if err := state.handleQuestion(context.Background(), nil, request); err == nil || !strings.Contains(err.Error(), "invalid native Question request") {
			t.Fatalf("invalid request %+v error=%v", request, err)
		}
	}

	state.nativeQuestions["changed"] = &nativeQuestionState{bindings: []nativeQuestionBinding{{questionID: "canonical-1", kind: "TEXT"}}}
	if err := state.handleQuestion(context.Background(), nil, client.QuestionRequest{
		ID:        "changed",
		SessionID: "ses_1",
		Questions: []client.QuestionInfo{{Question: "First"}, {Question: "Second"}},
	}); err == nil || !strings.Contains(err.Error(), "changed shape") {
		t.Fatalf("changed-shape error=%v", err)
	}

	state.nativeQuestions["done"] = &nativeQuestionState{replied: true}
	if err := state.handleQuestion(context.Background(), nil, client.QuestionRequest{
		ID: "done", SessionID: "ses_1", Questions: []client.QuestionInfo{{Question: "Already handled"}},
	}); err != nil {
		t.Fatalf("replied request should be idempotent: %v", err)
	}
}
