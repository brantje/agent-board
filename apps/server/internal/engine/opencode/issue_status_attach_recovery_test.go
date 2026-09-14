package opencode

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestEngineAttachContinuesWhenRecoveredIssueStatusIsSuperseded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{map[string]any{"id": "ses_existing"}})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"ses_existing": map[string]any{"type": "idle"}})
	})
	mux.HandleFunc("GET /question", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{})
	})
	mux.HandleFunc("GET /session/ses_existing/message", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{
			map[string]any{
				"info": map[string]any{"sessionID": "ses_existing", "role": "assistant"},
				"parts": []any{issueStatusToolPartPayload("ses_existing", "part_stale", "REVIEW")},
			},
		})
	})
	mux.HandleFunc("GET /api/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"id\":\"connected\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("POST /session/ses_existing/abort", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	process := newFakeOpenCodeProcess(parsed.Host)
	launcher := &fakeAttachLauncher{fakeOpenCodeLauncher: fakeOpenCodeLauncher{process: process}}
	statuses := &recordingIssueStatusUpdater{skipRecovered: true}
	adapter := newWithAddress(parsed.Host)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = adapter.Execute(ctx, engine.Request{
		Context: executioncontext.SafeContext{
			Issue:    executioncontext.IssueContext{Title: "Resume without stale Board overwrite"},
			Agent:    executioncontext.AgentContext{Engine: Name},
			Model:    executioncontext.ModelContext{Model: "test-model"},
			Provider: executioncontext.ProviderContext{Kind: "test-provider"},
		},
		Launcher:             launcher,
		InteractiveQuestions: &fakeInteractiveQuestions{},
		IssueStatus:          statuses,
	})
	if err != nil {
		t.Fatalf("Execute() stale recovery error=%v", err)
	}
	if launcher.starts != 0 || launcher.attaches != 1 {
		t.Fatalf("starts=%d attaches=%d", launcher.starts, launcher.attaches)
	}
	if len(statuses.recovered) != 1 || statuses.recovered[0] != "REVIEW" {
		t.Fatalf("recovered statuses=%v want [REVIEW]", statuses.recovered)
	}
	if len(statuses.statuses) != 0 {
		t.Fatalf("superseded historical status unexpectedly applied: %v", statuses.statuses)
	}
}
