package store

import "testing"

type questionStoreOnly struct{ QuestionStore }

type capabilityQuestionStore struct {
	QuestionStore
	supported bool
}

func (s capabilityQuestionStore) SupportsQuestionStore() bool { return s.supported }

func TestSupportsQuestionStore(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{name: "nil", value: nil, want: false},
		{name: "unrelated", value: struct{}{}, want: false},
		{name: "plain question store", value: questionStoreOnly{}, want: true},
		{name: "explicitly disabled", value: capabilityQuestionStore{supported: false}, want: false},
		{name: "explicitly enabled", value: capabilityQuestionStore{supported: true}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SupportsQuestionStore(tc.value); got != tc.want {
				t.Fatalf("SupportsQuestionStore()=%v want %v", got, tc.want)
			}
		})
	}
}
