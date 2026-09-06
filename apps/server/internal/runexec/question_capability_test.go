package runexec

import "testing"

type disabledQuestionExecutionStore struct {
	*orchestrationQuestionStore
}

func (*disabledQuestionExecutionStore) SupportsQuestionStore() bool { return false }

func TestEngineRequestHonorsExplicitQuestionCapability(t *testing.T) {
	base := &orchestrationQuestionStore{processTestStore: &processTestStore{}}
	processor := &Processor{store: &disabledQuestionExecutionStore{orchestrationQuestionStore: base}}
	request, err := processor.engineRequest(t.Context(), continuationSafeContext(), nil, "runtime-instance-1")
	if err != nil {
		t.Fatal(err)
	}
	if request.Questions != nil || request.Continuation != nil {
		t.Fatalf("request=%+v", request)
	}
}
