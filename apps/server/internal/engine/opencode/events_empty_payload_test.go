package opencode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestHandlePartUpdatedIgnoresMissingOptionalTelemetryPayload(t *testing.T) {
	state := newRunState("ses_1", nil, nil)
	for _, properties := range []json.RawMessage{
		nil,
		json.RawMessage(""),
		json.RawMessage("null"),
		json.RawMessage(" \n\t "),
	} {
		if err := state.handleEvent(context.Background(), nil, client.Event{
			Type:       "message.part.updated",
			Properties: properties,
		}); err != nil {
			t.Fatalf("empty optional telemetry payload %q returned error: %v", string(properties), err)
		}
	}
}
