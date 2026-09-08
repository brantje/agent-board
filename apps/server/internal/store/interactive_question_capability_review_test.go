package store

import (
	"context"
	"testing"
)

type batchStoreWithoutCapability struct{}

func (batchStoreWithoutCapability) OpenInteractiveQuestions(context.Context, []OpenInteractiveQuestionCommand) (OpenInteractiveQuestionsResult, error) {
	return OpenInteractiveQuestionsResult{}, nil
}

type batchStoreWithCapability struct {
	supported bool
}

func (batchStoreWithCapability) OpenInteractiveQuestions(context.Context, []OpenInteractiveQuestionCommand) (OpenInteractiveQuestionsResult, error) {
	return OpenInteractiveQuestionsResult{}, nil
}

func (s batchStoreWithCapability) SupportsInteractiveQuestionBatchStore() bool {
	return s.supported
}

func TestSupportsInteractiveQuestionBatchStore(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{name: "nil", value: nil, want: false},
		{name: "wrong interface", value: struct{}{}, want: false},
		{name: "implicit support", value: batchStoreWithoutCapability{}, want: true},
		{name: "explicit disabled", value: batchStoreWithCapability{supported: false}, want: false},
		{name: "explicit enabled", value: batchStoreWithCapability{supported: true}, want: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SupportsInteractiveQuestionBatchStore(testCase.value); got != testCase.want {
				t.Fatalf("SupportsInteractiveQuestionBatchStore()=%v want %v", got, testCase.want)
			}
		})
	}
}
