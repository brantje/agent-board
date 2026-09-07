package runexec

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestInteractiveQuestionerOpenDoesNotDuplicateStoreOwnedEvidence(t *testing.T) {
	eventStore := &questionEventStore{}
	recorder, err := evidence.NewRecorder(eventStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &interactiveLifecycleStore{openResult: store.OpenInteractiveQuestionResult{
		Question:       store.Question{ID: "question-1", Prompt: "Continue?", Kind: "TEXT", Blocking: true, Status: "OPEN"},
		Created:        true,
		EnteredWaiting: true,
		Events: []store.Event{
			{Type: "question.created"},
			{Type: "run.waiting_for_input"},
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

	opened, err := q.Open(context.Background(), "ses/req/0", engine.QuestionRequest{
		Prompt:   "Continue?",
		Kind:     "TEXT",
		Blocking: true,
	})
	if err != nil {
		t.Fatalf("Open() error=%v", err)
	}
	if opened.ID != "question-1" {
		t.Fatalf("opened=%+v", opened)
	}
	if len(eventStore.events) != 0 {
		t.Fatalf("store-owned lifecycle evidence was duplicated: %+v", eventStore.events)
	}
	if len(lifecycle.opened) != 1 || lifecycle.opened[0].RuntimeInstanceID != "runtime-instance-1" {
		t.Fatalf("open command=%+v", lifecycle.opened)
	}
}

func TestInteractiveQuestionerOpenBatchDoesNotDuplicateStoreOwnedEvidence(t *testing.T) {
	eventStore := &questionEventStore{}
	recorder, err := evidence.NewRecorder(eventStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &interactiveBatchLifecycleStore{result: store.OpenInteractiveQuestionsResult{
		EnteredWaiting: true,
		Questions: []store.OpenInteractiveQuestionResult{{
			Question:       store.Question{ID: "question-1", Prompt: "Continue?", Kind: "TEXT", Blocking: true, Status: "OPEN"},
			Created:        true,
			EnteredWaiting: true,
		}},
		Events: []store.Event{
			{Type: "question.created"},
			{Type: "run.waiting_for_input"},
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

	opened, err := q.OpenBatch(context.Background(), []engine.CorrelatedQuestionRequest{{
		CorrelationKey: "ses/req/0",
		Question: engine.QuestionRequest{
			Prompt:   "Continue?",
			Kind:     "TEXT",
			Blocking: true,
		},
	}})
	if err != nil {
		t.Fatalf("OpenBatch() error=%v", err)
	}
	if len(opened) != 1 || opened[0].ID != "question-1" {
		t.Fatalf("opened=%+v", opened)
	}
	if len(eventStore.events) != 0 {
		t.Fatalf("store-owned lifecycle evidence was duplicated: %+v", eventStore.events)
	}
	if len(lifecycle.batches) != 1 || len(lifecycle.batches[0]) != 1 || lifecycle.batches[0][0].RuntimeInstanceID != "runtime-instance-1" {
		t.Fatalf("batch commands=%+v", lifecycle.batches)
	}
}
