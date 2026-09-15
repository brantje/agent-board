package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestEnsureNativeSessionCreatesWorkspaceBoundSession(t *testing.T) {
	var createCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, r *http.Request) {
		createCalls.Add(1)
		var payload struct {
			Model    client.ModelRef `json:"model"`
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode create session: %v", err)
		}
		if payload.Location.Directory != "/workspace" {
			t.Fatalf("session directory=%q want /workspace", payload.Location.Directory)
		}
		if payload.Model.ProviderID != "anthropic" || payload.Model.ID != "claude-sonnet" || payload.Model.Variant != "thinking" {
			t.Fatalf("session model=%+v", payload.Model)
		}
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "ses_new", "directory": "/workspace"}})
	})

	native := newWorkspaceTestClient(t, mux)
	session, promptRequired, err := ensureNativeSession(context.Background(), native, executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "claude-sonnet"},
	}, settings{ProviderID: "anthropic", Variant: "thinking"}, false)
	if err != nil {
		t.Fatalf("ensureNativeSession() error=%v", err)
	}
	if !promptRequired || session.ID != "ses_new" || session.Directory != "/workspace" || createCalls.Load() != 1 {
		t.Fatalf("session=%+v promptRequired=%v createCalls=%d", session, promptRequired, createCalls.Load())
	}
}

func TestEnsureNativeSessionRecoverySkipsOtherDirectories(t *testing.T) {
	var createCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{
			map[string]any{"id": "ses_other", "directory": "/home/runner/projects/issue"},
			map[string]any{"id": "ses_workspace", "directory": "/workspace"},
		})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{
			"ses_other":     map[string]any{"type": "busy"},
			"ses_workspace": map[string]any{"type": "idle"},
		})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, _ *http.Request) {
		createCalls.Add(1)
		http.Error(w, "unexpected create", http.StatusInternalServerError)
	})

	native := newWorkspaceTestClient(t, mux)
	session, promptRequired, err := ensureNativeSession(context.Background(), native, executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "test-model"},
	}, settings{ProviderID: "test-provider"}, true)
	if err != nil {
		t.Fatalf("ensureNativeSession() error=%v", err)
	}
	if promptRequired || session.ID != "ses_workspace" || session.Directory != "/workspace" || createCalls.Load() != 0 {
		t.Fatalf("session=%+v promptRequired=%v createCalls=%d", session, promptRequired, createCalls.Load())
	}
}

func TestEnsureNativeSessionRecoveryCreatesWorkspaceSessionWhenNoWorkspaceSessionExists(t *testing.T) {
	var createCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{map[string]any{"id": "ses_other", "directory": "/tmp/other-project"}})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, r *http.Request) {
		createCalls.Add(1)
		var payload struct {
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode create session: %v", err)
		}
		if payload.Location.Directory != "/workspace" {
			t.Fatalf("session directory=%q want /workspace", payload.Location.Directory)
		}
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "ses_new", "directory": "/workspace"}})
	})

	native := newWorkspaceTestClient(t, mux)
	session, promptRequired, err := ensureNativeSession(context.Background(), native, executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "test-model"},
	}, settings{ProviderID: "test-provider"}, true)
	if err != nil {
		t.Fatalf("ensureNativeSession() error=%v", err)
	}
	if !promptRequired || session.ID != "ses_new" || session.Directory != "/workspace" || createCalls.Load() != 1 {
		t.Fatalf("session=%+v promptRequired=%v createCalls=%d", session, promptRequired, createCalls.Load())
	}
}

func newWorkspaceTestClient(t *testing.T, handler http.Handler) *client.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("client.New() error=%v", err)
	}
	return native
}
