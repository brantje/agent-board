package runexec

import (
	"context"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type capabilityRunEventReader struct{}

func (capabilityRunEventReader) ListRunEvents(context.Context, string, string, int64, int) ([]store.Event, error) {
	return nil, nil
}

func TestExposeInteractiveQuestionerHidesReplyTrackerWithoutEventReader(t *testing.T) {
	questioner := &interactiveQuestioner{}
	exposed := exposeInteractiveQuestioner(questioner)
	if _, ok := exposed.(engine.InteractiveQuestionBatcher); !ok {
		t.Fatal("batch capability was hidden with reply tracking")
	}
	if _, ok := exposed.(engine.InteractiveQuestionReplyTracker); ok {
		t.Fatal("reply tracker exposed without run-event reader")
	}

	questioner.eventReader = capabilityRunEventReader{}
	exposed = exposeInteractiveQuestioner(questioner)
	if _, ok := exposed.(engine.InteractiveQuestionReplyTracker); !ok {
		t.Fatal("reply tracker not exposed with run-event reader")
	}
}

func TestInteractiveQuestionerOpenBatchFallsBackToSingleOpen(t *testing.T) {
	eventStore := &questionEventStore{}
	recorder, err := evidence.NewRecorder(eventStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &interactiveLifecycleStore{openResult: store.OpenInteractiveQuestionResult{
		Question: store.Question{ID: "question-1", Prompt: "Continue?", Kind: "TEXT", Blocking: true, Status: "OPEN"},
	}}
	questioner := &interactiveQuestioner{
		store:       &interactiveQuestionReadStore{},
		interactive: lifecycle,
		events:      recorder,
		safe:        interactiveSafeContext(),
		engine:      "opencode",
	}

	opened, err := questioner.OpenBatch(context.Background(), []engine.CorrelatedQuestionRequest{{
		CorrelationKey: "session/request/0",
		Question:       engine.QuestionRequest{Prompt: "Continue?", Kind: "TEXT", Blocking: true},
	}})
	if err != nil {
		t.Fatalf("single OpenBatch() error=%v", err)
	}
	if len(opened) != 1 || opened[0].ID != "question-1" || len(lifecycle.opened) != 1 {
		t.Fatalf("single fallback opened=%+v commands=%+v", opened, lifecycle.opened)
	}

	_, err = questioner.OpenBatch(context.Background(), []engine.CorrelatedQuestionRequest{
		{CorrelationKey: "session/request/0", Question: engine.QuestionRequest{Prompt: "First?", Kind: "TEXT", Blocking: true}},
		{CorrelationKey: "session/request/1", Question: engine.QuestionRequest{Prompt: "Second?", Kind: "TEXT", Blocking: true}},
	})
	if err == nil || !strings.Contains(err.Error(), "batch capability is unavailable") {
		t.Fatalf("multi-question fallback error=%v", err)
	}
}
