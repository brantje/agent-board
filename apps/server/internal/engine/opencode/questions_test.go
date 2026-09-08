package opencode

import (
	"reflect"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestMapNativeQuestionPreservesSupportedAnswerKinds(t *testing.T) {
	multiple := true
	notMultiple := false
	customDisabled := false
	tests := []struct {
		name   string
		native client.QuestionInfo
		kind   string
		custom bool
		labels map[string]string
	}{
		{
			name:   "text",
			native: client.QuestionInfo{Question: "Explain the tradeoff"},
			kind:   "TEXT",
			custom: true,
			labels: map[string]string{},
		},
		{
			name: "single choice defaults custom answers on",
			native: client.QuestionInfo{
				Question: "Choose one",
				Multiple: &notMultiple,
				Options: []client.QuestionOption{
					{Label: "First", Description: "first option"},
					{Label: "Second", Description: "second option"},
				},
			},
			kind:   "SINGLE_CHOICE",
			custom: true,
			labels: map[string]string{"option-0": "First", "option-1": "Second"},
		},
		{
			name: "multi choice",
			native: client.QuestionInfo{
				Question: "Choose several",
				Multiple: &multiple,
				Options: []client.QuestionOption{
					{Label: "First", Description: "first option"},
					{Label: "Second", Description: "second option"},
				},
			},
			kind:   "MULTI_CHOICE",
			custom: true,
			labels: map[string]string{"option-0": "First", "option-1": "Second"},
		},
		{
			name: "explicit custom disabled",
			native: client.QuestionInfo{
				Question: "Choose one",
				Custom:   &customDisabled,
				Options:  []client.QuestionOption{{Label: "First"}},
			},
			kind:   "SINGLE_CHOICE",
			custom: false,
			labels: map[string]string{"option-0": "First"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			mapped, labels, err := mapNativeQuestion(testCase.native)
			if err != nil {
				t.Fatalf("mapNativeQuestion() error=%v", err)
			}
			if mapped.Kind != testCase.kind || !mapped.Blocking || mapped.Custom != testCase.custom {
				t.Fatalf("mapped=%+v", mapped)
			}
			if !reflect.DeepEqual(labels, testCase.labels) {
				t.Fatalf("labels=%v want %v", labels, testCase.labels)
			}
		})
	}
}

func TestMapCanonicalAnswerRoundTripsLabelsAndText(t *testing.T) {
	text := "Use the safer implementation"
	custom := "Something else"
	tests := []struct {
		name    string
		binding nativeQuestionBinding
		answer  engine.QuestionAnswer
		want    []string
	}{
		{
			name:    "text",
			binding: nativeQuestionBinding{kind: "TEXT"},
			answer:  engine.QuestionAnswer{Kind: "TEXT", Text: &text},
			want:    []string{text},
		},
		{
			name: "single choice",
			binding: nativeQuestionBinding{
				kind:   "SINGLE_CHOICE",
				labels: map[string]string{"option-0": "First", "option-1": "Second"},
			},
			answer: engine.QuestionAnswer{Kind: "SINGLE_CHOICE", OptionIDs: []string{"option-1"}},
			want:   []string{"Second"},
		},
		{
			name: "multi choice",
			binding: nativeQuestionBinding{
				kind:   "MULTI_CHOICE",
				labels: map[string]string{"option-0": "First", "option-1": "Second"},
			},
			answer: engine.QuestionAnswer{Kind: "MULTI_CHOICE", OptionIDs: []string{"option-1", "option-0"}},
			want:   []string{"Second", "First"},
		},
		{
			name:    "single choice custom text",
			binding: nativeQuestionBinding{kind: "SINGLE_CHOICE", custom: true, labels: map[string]string{"option-0": "First"}},
			answer:  engine.QuestionAnswer{Kind: "SINGLE_CHOICE", Text: &custom},
			want:    []string{custom},
		},
		{
			name:    "multi choice custom text",
			binding: nativeQuestionBinding{kind: "MULTI_CHOICE", custom: true, labels: map[string]string{"option-0": "First"}},
			answer:  engine.QuestionAnswer{Kind: "MULTI_CHOICE", Text: &custom},
			want:    []string{custom},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := mapCanonicalAnswer(testCase.binding, testCase.answer)
			if err != nil {
				t.Fatalf("mapCanonicalAnswer() error=%v", err)
			}
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("answer=%v want %v", got, testCase.want)
			}
		})
	}
}

func TestMapCanonicalAnswerRejectsStaleOrMismatchedChoice(t *testing.T) {
	custom := "Other"
	binding := nativeQuestionBinding{
		kind:   "SINGLE_CHOICE",
		labels: map[string]string{"option-0": "First"},
	}
	for _, answer := range []engine.QuestionAnswer{
		{Kind: "TEXT", Text: stringPointer("First")},
		{Kind: "SINGLE_CHOICE", OptionIDs: []string{"option-stale"}},
		{Kind: "SINGLE_CHOICE", Text: &custom},
	} {
		if _, err := mapCanonicalAnswer(binding, answer); err == nil {
			t.Fatalf("answer %+v unexpectedly accepted", answer)
		}
	}
}

func stringPointer(value string) *string { return &value }
