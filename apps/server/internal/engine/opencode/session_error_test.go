package opencode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestHandleSessionErrorFailsOnlyOwnedNativeSession(t *testing.T) {
	state := newRunState("ses_1", nil, nil)

	other := mustJSON(t, map[string]any{
		"sessionID": "ses_other",
		"error": map[string]any{"name": "UnknownError", "data": map[string]any{"message": "ignore me"}},
	})
	if err := state.handleEvent(context.Background(), nil, client.Event{Type: "session.error", Properties: other}); err != nil {
		t.Fatalf("unrelated session.error returned %v", err)
	}

	owned := mustJSON(t, map[string]any{
		"sessionID": "ses_1",
		"error": map[string]any{"name": "ProviderAuthError", "data": map[string]any{"message": "provider authentication failed"}},
	})
	err := state.handleEvent(context.Background(), nil, client.Event{Type: "session.error", Properties: owned})
	if err == nil || !strings.Contains(err.Error(), "provider authentication failed") {
		t.Fatalf("owned session.error=%v", err)
	}
}

func TestHandleSessionErrorDoesNotExposeUnneededNativeFields(t *testing.T) {
	state := newRunState("ses_1", nil, nil)
	properties := mustJSON(t, map[string]any{
		"sessionID": "ses_1",
		"error": map[string]any{
			"name": "APIError",
			"data": map[string]any{
				"message":      "request failed",
				"responseBody": "sensitive upstream body",
			},
		},
	})
	err := state.handleSessionError(properties)
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("session.error=%v", err)
	}
	if strings.Contains(err.Error(), "sensitive upstream body") {
		t.Fatalf("session.error leaked response body: %v", err)
	}
}

func TestHandleSessionErrorRejectsMalformedControlPayload(t *testing.T) {
	state := newRunState("ses_1", nil, nil)
	if err := state.handleEvent(context.Background(), nil, client.Event{Type: "session.error", Properties: json.RawMessage("{")}); err == nil {
		t.Fatal("malformed session.error unexpectedly accepted")
	}
	if err := state.handleSessionError(mustJSON(t, map[string]any{"sessionID": "ses_1", "error": json.RawMessage("{")})); err == nil {
		t.Fatal("malformed native error details unexpectedly accepted")
	}
}
