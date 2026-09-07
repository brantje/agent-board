package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestIsSessionIdleEventFiltersAndValidatesNativeSession(t *testing.T) {
	idle, err := isSessionIdleEvent(client.Event{Type: "message.part.updated"}, "ses_1")
	if err != nil || idle {
		t.Fatalf("non-idle event idle=%v err=%v", idle, err)
	}

	idle, err = isSessionIdleEvent(client.Event{
		Type:       "session.idle",
		Properties: idleEventProperties(t, "ses_other"),
	}, "ses_1")
	if err != nil || idle {
		t.Fatalf("other session idle=%v err=%v", idle, err)
	}

	idle, err = isSessionIdleEvent(client.Event{
		Type:       "session.idle",
		Properties: idleEventProperties(t, "ses_1"),
	}, "ses_1")
	if err != nil || !idle {
		t.Fatalf("matching session idle=%v err=%v", idle, err)
	}

	if _, err := isSessionIdleEvent(client.Event{
		Type:       "session.idle",
		Properties: json.RawMessage("{"),
	}, "ses_1"); err == nil || !strings.Contains(err.Error(), "decode session idle event") {
		t.Fatalf("malformed idle event error=%v", err)
	}
}

func TestReconcilePendingQuestionsHandlesEmptyAndErrorResponses(t *testing.T) {
	emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"data": []any{}})
	}))
	defer emptyServer.Close()
	native, err := client.New(emptyServer.Client(), emptyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	state := newRunState("ses_1", &fakeInteractiveQuestions{}, nil)
	hadPending, err := reconcilePendingQuestions(context.Background(), native, "ses_1", state)
	if err != nil || hadPending {
		t.Fatalf("empty reconciliation pending=%v err=%v", hadPending, err)
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer errorServer.Close()
	native, err = client.New(errorServer.Client(), errorServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconcilePendingQuestions(context.Background(), native, "ses_1", state); err == nil || !strings.Contains(err.Error(), "reconcile pending Questions") {
		t.Fatalf("reconciliation error=%v", err)
	}
}

func idleEventProperties(t *testing.T, sessionID string) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(map[string]string{"sessionID": sessionID})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
