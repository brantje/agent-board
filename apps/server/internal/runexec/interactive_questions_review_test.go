package runexec

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type interactiveBatchLifecycleStore struct {
	batches [][]store.OpenInteractiveQuestionCommand
	result  store.OpenInteractiveQuestionsResult
	err     error
}

func (*interactiveBatchLifecycleStore) OpenInteractiveQuestion(context.Context, store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionResult, error) {
	return store.OpenInteractiveQuestionResult{}, nil
}

func (s *interactiveBatchLifecycleStore) OpenInteractiveQuestions(_ context.Context, commands []store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionsResult, error) {
	copied := append([]store.OpenInteractiveQuestionCommand(nil), commands...)
	s.batches = append(s.batches, copied)
	return s.result, s.err
}

func (*interactiveBatchLifecycleStore) ResolveInteractiveQuestion(context.Context, string, string) (store.ResolveInteractiveQuestionResult, error) {
	return store.ResolveInteractiveQuestionResult{}, nil
}

func TestInteractiveQuestionerOpenBatchUsesAtomicStoreAndOrdersEvidence(t *testing.T) {
	eventStore := &questionEventStore{}
	recorder, err := evidence.NewRecorder(eventStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &interactiveBatchLifecycleStore{result: store.OpenInteractiveQuestionsResult{
		EnteredWaiting: true,
		Questions: []store.OpenInteractiveQuestionResult{
			{
				Question:       store.Question{ID: "question-1", Prompt: "First?", Kind: "TEXT", Blocking: true, Status: "OPEN"},
				Created:        true,
				EnteredWaiting: true,
			},
			{
				Question: store.Question{ID: "question-2", Prompt: "Second?", Kind: "TEXT", Blocking: true, Status: "OPEN"},
				Created:  true,
			},
		},
	}}
	q := &interactiveQuestioner{
		store:             &interactiveQuestionReadStore{},
		interactive:       lifecycle,
		events:            recorder,
		safe:              interactiveSafeContext(),
		runtimeInstanceID: "runtime-instance-1",
		engine:            "opencode",
	}

	opened, err := q.OpenBatch(context.Background(), []engine.CorrelatedQuestionRequest{
		{CorrelationKey: "ses/req/0", Question: engine.QuestionRequest{Prompt: "First?", Kind: "TEXT", Blocking: true}},
		{CorrelationKey: "ses/req/1", Question: engine.QuestionRequest{Prompt: "Second?", Kind: "TEXT", Blocking: true}},
	})
	if err != nil {
		t.Fatalf("OpenBatch() error=%v", err)
	}
	if len(opened) != 2 || opened[0].ID != "question-1" || opened[1].ID != "question-2" {
		t.Fatalf("opened=%+v", opened)
	}
	if len(lifecycle.batches) != 1 || len(lifecycle.batches[0]) != 2 {
		t.Fatalf("batches=%+v", lifecycle.batches)
	}
	if got := lifecycle.batches[0][1].CorrelationKey; got != "ses/req/1" {
		t.Fatalf("second correlation=%q", got)
	}
	if len(eventStore.events) != 3 {
		t.Fatalf("events=%+v", eventStore.events)
	}
	if eventStore.events[0].Type != "question.created" || eventStore.events[1].Type != "question.created" || eventStore.events[2].Type != "run.waiting_for_input" {
		t.Fatalf("event order=%+v", eventStore.events)
	}
}

func TestNextInteractiveQuestionPollIntervalBacksOffAndCaps(t *testing.T) {
	interval := interactiveQuestionPollInitialInterval
	want := []time.Duration{
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
		2 * time.Second,
		2 * time.Second,
	}
	for index, expected := range want {
		interval = nextInteractiveQuestionPollInterval(interval)
		if interval != expected {
			t.Fatalf("step %d interval=%s want %s", index, interval, expected)
		}
	}
}

var _ store.InteractiveQuestionStore = (*interactiveBatchLifecycleStore)(nil)
var _ store.InteractiveQuestionBatchStore = (*interactiveBatchLifecycleStore)(nil)
