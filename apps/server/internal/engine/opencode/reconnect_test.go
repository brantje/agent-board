package opencode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

type reconnectHarness struct {
	mu            sync.Mutex
	subscriptions int
	promptCalls   int
	replied       bool
	replyAnswers  [][]string
	question      map[string]any
}

func newReconnectHarness() *reconnectHarness {
	return &reconnectHarness{
		question: map[string]any{
			"id":        "que_missed",
			"sessionID": "ses_native",
			"questions": []any{
				map[string]any{
					"question": "Which implementation?",
					"header":   "Implementation",
					"options": []any{
						map[string]any{"label": "A", "description": "first"},
						map[string]any{"label": "B", "description": "second"},
					},
				},
			},
		},
	}
}

func (h *reconnectHarness) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "ses_native"}})
	})
	mux.HandleFunc("GET /api/event", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.subscriptions++
		subscription := h.subscriptions
		h.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("response writer does not flush")
			return
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"connected\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		flusher.Flush()
		if subscription == 1 {
			// Simulate a transport loss before question.v2.asked reaches Agent Board.
			// The authoritative pending-Question endpoint must recover it.
			return
		}
		<-r.Context().Done()
	})
	mux.HandleFunc("POST /api/session/ses_native/prompt", func(w http.ResponseWriter, _ *http.Request) {
		h.mu.Lock()
		h.promptCalls++
		h.mu.Unlock()
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "input_native"}})
	})
	mux.HandleFunc("GET /api/session/active", func(w http.ResponseWriter, _ *http.Request) {
		h.mu.Lock()
		active := !h.replied || h.subscriptions < 2
		h.mu.Unlock()
		if active {
			writeNativeJSON(t, w, map[string]any{"data": map[string]any{"ses_native": map[string]any{"type": "running"}}})
			return
		}
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{}})
	})
	mux.HandleFunc("GET /api/session/ses_native/question", func(w http.ResponseWriter, _ *http.Request) {
		h.mu.Lock()
		replied := h.replied
		prompted := h.promptCalls > 0
		h.mu.Unlock()
		if !prompted || replied {
			writeNativeJSON(t, w, map[string]any{"data": []any{}})
			return
		}
		writeNativeJSON(t, w, map[string]any{"data": []any{h.question}})
	})
	mux.HandleFunc("POST /api/session/ses_native/question/que_missed/reply", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Answers [][]string `json:"answers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode native answer: %v", err)
		}
		h.mu.Lock()
		h.replied = true
		h.replyAnswers = payload.Answers
		h.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/session/ses_native/interrupt", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/session/ses_native/question/que_missed/reject", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func TestEngineReconcilesMissedQuestionAfterEventStreamDisconnect(t *testing.T) {
	harness := newReconnectHarness()
	server := httptest.NewServer(harness.handler(t))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	process := newFakeOpenCodeProcess(parsed.Host)
	launcher := &fakeOpenCodeLauncher{process: process}
	questions := &fakeInteractiveQuestions{}
	adapter := newWithAddress(parsed.Host)

	executeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = adapter.Execute(executeCtx, engine.Request{
		Context: executioncontext.SafeContext{
			Issue:    executioncontext.IssueContext{Title: "Recover the missed Question"},
			Executor: executioncontext.ExecutorContext{Engine: Name},
			Model:    executioncontext.ModelContext{Model: "claude-sonnet"},
			Provider: executioncontext.ProviderContext{Kind: "anthropic"},
		},
		Launcher:             launcher,
		InteractiveQuestions: questions,
	})
	if err != nil {
		t.Fatalf("Execute() error=%v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if launcher.starts != 1 || harness.promptCalls != 1 {
		t.Fatalf("server starts=%d prompt calls=%d", launcher.starts, harness.promptCalls)
	}
	if harness.subscriptions < 2 {
		t.Fatalf("event subscriptions=%d want reconnect", harness.subscriptions)
	}
	if len(questions.opened) != 1 || len(questions.resolved) != 1 {
		t.Fatalf("opened=%v resolved=%v", questions.opened, questions.resolved)
	}
	if len(harness.replyAnswers) != 1 || len(harness.replyAnswers[0]) != 1 || harness.replyAnswers[0][0] != "B" {
		t.Fatalf("native answers=%v", harness.replyAnswers)
	}
	if questions.correlations[0] != "ses_native/que_missed/0" {
		t.Fatalf("correlations=%v", questions.correlations)
	}
}
