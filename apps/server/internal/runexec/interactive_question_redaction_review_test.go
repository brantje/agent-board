package runexec

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestInteractiveQuestionCreatedEvidenceUsesPersistedRedactedOptions(t *testing.T) {
	persistedOptions, err := json.Marshal([]store.QuestionOption{{ID: "option-0", Label: "[REDACTED]"}})
	if err != nil {
		t.Fatal(err)
	}
	persisted := store.Question{
		ID:       "question-redacted",
		Prompt:   "Choose [REDACTED]",
		Kind:     "SINGLE_CHOICE",
		Options:  persistedOptions,
		Blocking: true,
		Status:   "OPEN",
	}
	request := engine.QuestionRequest{
		Prompt:   "Choose secret prompt",
		Kind:     "SINGLE_CHOICE",
		Blocking: true,
		Options:  []engine.QuestionOption{{ID: "option-0", Label: "secret-label"}},
	}

	t.Run("single", func(t *testing.T) {
		events := &questionEventStore{}
		recorder, err := evidence.NewRecorder(events, nil)
		if err != nil {
			t.Fatal(err)
		}
		lifecycle := &interactiveLifecycleStore{openResult: store.OpenInteractiveQuestionResult{Question: persisted, Created: true}}
		q := &interactiveQuestioner{
			store:       &interactiveQuestionReadStore{},
			interactive: lifecycle,
			events:      recorder,
			safe:        interactiveSafeContext(),
			engine:      "opencode",
		}
		if _, err := q.Open(context.Background(), "ses/req/0", request); err != nil {
			t.Fatalf("Open() error=%v", err)
		}
		assertRedactedQuestionCreatedEvent(t, events.events, "secret-label")
	})

	t.Run("batch", func(t *testing.T) {
		events := &questionEventStore{}
		recorder, err := evidence.NewRecorder(events, nil)
		if err != nil {
			t.Fatal(err)
		}
		lifecycle := &interactiveBatchLifecycleStore{result: store.OpenInteractiveQuestionsResult{
			Questions: []store.OpenInteractiveQuestionResult{{Question: persisted, Created: true}},
		}}
		q := &interactiveQuestioner{
			store:       &interactiveQuestionReadStore{},
			interactive: lifecycle,
			events:      recorder,
			safe:        interactiveSafeContext(),
			engine:      "opencode",
		}
		if _, err := q.OpenBatch(context.Background(), []engine.CorrelatedQuestionRequest{{CorrelationKey: "ses/req/0", Question: request}}); err != nil {
			t.Fatalf("OpenBatch() error=%v", err)
		}
		assertRedactedQuestionCreatedEvent(t, events.events, "secret-label")
	})
}

func assertRedactedQuestionCreatedEvent(t *testing.T, events []store.Event, forbidden string) {
	t.Helper()
	if len(events) != 1 || events[0].Type != "question.created" {
		t.Fatalf("events=%+v", events)
	}
	if strings.Contains(string(events[0].Payload), forbidden) {
		t.Fatalf("question.created leaked pre-redaction option label: %s", events[0].Payload)
	}
	var payload struct {
		Prompt  string                 `json:"prompt"`
		Options []store.QuestionOption `json:"options"`
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Prompt != "Choose [REDACTED]" || len(payload.Options) != 1 || payload.Options[0].Label != "[REDACTED]" {
		t.Fatalf("payload=%+v", payload)
	}
}
