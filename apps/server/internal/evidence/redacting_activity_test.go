package evidence

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRedactingStoreSanitizesStructuredAgentActivity(t *testing.T) {
	registry := redaction.NewRegistry()
	const runID = "run-activity"
	const secret = "super-secret-token"
	registry.Register(runID, []string{secret})
	base := &captureStore{}
	secured := NewRedactingStore(base, registry)

	payload, err := json.Marshal(map[string]any{
		"message": "reasoning mentions " + secret,
		"kind":    "reasoning",
		"input": map[string]any{
			"headers": map[string]any{"Authorization": "Bearer " + secret},
			"items":   []any{"safe", secret},
		},
		"summary":       "using " + secret,
		"resultPreview": "result " + secret,
		"reason":        "failed with " + secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	eventRunID := runID
	if _, err := secured.AppendEvent(context.Background(), store.Event{RunID: &eventRunID, Actor: store.EmptyObject, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(base.event.Payload), secret) {
		t.Fatalf("structured activity leaked secret: %s", base.event.Payload)
	}
	if !strings.Contains(string(base.event.Payload), "reasoning") {
		t.Fatalf("activity shape was not preserved: %s", base.event.Payload)
	}
}
