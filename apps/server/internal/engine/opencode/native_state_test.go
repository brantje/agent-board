package opencode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
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

func TestEngineCompletesOnMatchingNativeIdleEvent(t *testing.T) {
	idleEvents := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "ses_idle"}})
	})
	mux.HandleFunc("GET /api/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"id\":\"connected\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-idleEvents:
		}
		properties := idleEventProperties(t, "ses_idle")
		event, err := json.Marshal(map[string]any{"id": "evt_idle", "type": "session.idle", "properties": properties})
		if err != nil {
			t.Errorf("marshal idle event: %v", err)
			return
		}
		_, _ = io.WriteString(w, "data: "+string(event)+"\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("POST /api/session/ses_idle/prompt", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "input_idle"}})
		idleEvents <- struct{}{}
	})
	mux.HandleFunc("GET /api/session/ses_idle/question", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"data": []any{}})
	})
	mux.HandleFunc("POST /api/session/ses_idle/interrupt", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	process := newFakeOpenCodeProcess(parsed.Host)
	launcher := &fakeOpenCodeLauncher{process: process}
	adapter := newWithAddress(parsed.Host)

	_, err = adapter.Execute(context.Background(), engine.Request{
		Context: executioncontext.SafeContext{
			Issue:    executioncontext.IssueContext{Title: "Complete on native idle"},
			Executor: executioncontext.ExecutorContext{Engine: Name},
			Model:    executioncontext.ModelContext{Model: "test-model"},
			Provider: executioncontext.ProviderContext{Kind: "test-provider"},
		},
		Launcher:             launcher,
		InteractiveQuestions: &fakeInteractiveQuestions{},
	})
	if err != nil {
		t.Fatalf("Execute() error=%v", err)
	}
	if launcher.starts != 1 {
		t.Fatalf("server starts=%d want 1", launcher.starts)
	}
}

func TestSessionActivePropagatesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := native.SessionActive(context.Background(), "ses_1"); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("SessionActive error=%v", err)
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
