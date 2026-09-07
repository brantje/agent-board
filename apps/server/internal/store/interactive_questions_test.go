package store

import (
	"context"
	"testing"
)

type interactiveCapabilityStore struct {
	supported *bool
}

func (*interactiveCapabilityStore) OpenInteractiveQuestion(context.Context, OpenInteractiveQuestionCommand) (OpenInteractiveQuestionResult, error) {
	return OpenInteractiveQuestionResult{}, nil
}

func (*interactiveCapabilityStore) ResolveInteractiveQuestion(context.Context, string, string) (ResolveInteractiveQuestionResult, error) {
	return ResolveInteractiveQuestionResult{}, nil
}

func (s *interactiveCapabilityStore) SupportsInteractiveQuestionStore() bool {
	return s.supported != nil && *s.supported
}

type plainInteractiveStore struct{}

func (*plainInteractiveStore) OpenInteractiveQuestion(context.Context, OpenInteractiveQuestionCommand) (OpenInteractiveQuestionResult, error) {
	return OpenInteractiveQuestionResult{}, nil
}

func (*plainInteractiveStore) ResolveInteractiveQuestion(context.Context, string, string) (ResolveInteractiveQuestionResult, error) {
	return ResolveInteractiveQuestionResult{}, nil
}

func TestSupportsInteractiveQuestionStore(t *testing.T) {
	yes, no := true, false
	for _, testCase := range []struct {
		name  string
		value any
		want  bool
	}{
		{name: "nil", value: nil, want: false},
		{name: "unrelated", value: struct{}{}, want: false},
		{name: "plain implementation", value: &plainInteractiveStore{}, want: true},
		{name: "capability enabled", value: &interactiveCapabilityStore{supported: &yes}, want: true},
		{name: "capability disabled", value: &interactiveCapabilityStore{supported: &no}, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SupportsInteractiveQuestionStore(testCase.value); got != testCase.want {
				t.Fatalf("SupportsInteractiveQuestionStore()=%v want %v", got, testCase.want)
			}
		})
	}
}
