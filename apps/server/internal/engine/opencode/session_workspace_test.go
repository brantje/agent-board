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

const hostRunnerWorkspace = "/tmp/external-runner-workspaces/session-1"

func TestEnsureNativeSessionBindsToRunnerWorkingDirectory(t *testing.T) {
	var createCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, r *http.Request) {
		createCalls.Add(1)
		if got := r.URL.Query().Get("directory"); got != hostRunnerWorkspace {
			t.Fatalf("create directory query=%q want %q", got, hostRunnerWorkspace)
		}
		if got := r.Header.Get("x-opencode-directory"); got != hostRunnerWorkspace {
			t.Fatalf("create x-opencode-directory=%q want %q", got, hostRunnerWorkspace)
		}
		var payload struct {
			Model    client.ModelRef `json:"model"`
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode create session: %v", err)
		}
		if payload.Location.Directory != hostRunnerWorkspace {
			t.Fatalf("session directory=%q want runner working directory", payload.Location.Directory)
		}
		if payload.Model.ProviderID != "anthropic" || payload.Model.ID != "claude-sonnet" || payload.Model.Variant != "thinking" {
			t.Fatalf("session model=%+v", payload.Model)
		}
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "ses_new", "directory": hostRunnerWorkspace}})
	})

	native := newWorkspaceTestClient(t, mux, hostRunnerWorkspace)
	session, promptRequired, err := ensureNativeSession(context.Background(), native, executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "claude-sonnet"},
	}, settings{ProviderID: "anthropic", Variant: "thinking"}, "expected prompt", false)
	if err != nil {
		t.Fatalf("ensureNativeSession() error=%v", err)
	}
	if !promptRequired || session.ID != "ses_new" || session.Directory != hostRunnerWorkspace || createCalls.Load() != 1 {
		t.Fatalf("session=%+v promptRequired=%v createCalls=%d", session, promptRequired, createCalls.Load())
	}
}

func TestEnsureNativeSessionRecoverySkipsHomeProjectsIssue(t *testing.T) {
	var createCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("directory"); got != hostRunnerWorkspace {
			t.Fatalf("list directory query=%q want %q", got, hostRunnerWorkspace)
		}
		writeNativeJSON(t, w, []any{
			map[string]any{"id": "ses_other", "directory": "/home/runner/projects/issue"},
			map[string]any{"id": "ses_host", "directory": hostRunnerWorkspace},
		})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("directory"); got != hostRunnerWorkspace {
			t.Fatalf("status directory query=%q want %q", got, hostRunnerWorkspace)
		}
		writeNativeJSON(t, w, map[string]any{
			"ses_other": map[string]any{"type": "busy"},
			"ses_host":  map[string]any{"type": "idle"},
		})
	})
	mux.HandleFunc("GET /session/ses_host/message", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{map[string]any{
			"info": map[string]any{"role": "user"},
			"parts": []any{map[string]any{"type": "text", "text": "expected prompt"}},
		}})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, _ *http.Request) {
		createCalls.Add(1)
		http.Error(w, "unexpected create", http.StatusInternalServerError)
	})

	native := newWorkspaceTestClient(t, mux, hostRunnerWorkspace)
	session, promptRequired, err := ensureNativeSession(context.Background(), native, executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "test-model"},
	}, settings{ProviderID: "test-provider"}, "expected prompt", true)
	if err != nil {
		t.Fatalf("ensureNativeSession() error=%v", err)
	}
	if promptRequired || session.ID != "ses_host" || session.Directory != hostRunnerWorkspace || createCalls.Load() != 0 {
		t.Fatalf("session=%+v promptRequired=%v createCalls=%d", session, promptRequired, createCalls.Load())
	}
}

func TestEnsureNativeSessionRecoveryPromptsExistingSessionWithEmptyHistory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{map[string]any{"id": "ses_existing", "directory": hostRunnerWorkspace}})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"ses_existing": map[string]any{"type": "idle"}})
	})
	mux.HandleFunc("GET /session/ses_existing/message", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{})
	})
	native := newWorkspaceTestClient(t, mux, hostRunnerWorkspace)
	session, promptRequired, err := ensureNativeSession(t.Context(), native, executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "test-model"},
	}, settings{ProviderID: "test-provider"}, "expected prompt", true)
	if err != nil {
		t.Fatal(err)
	}
	if !promptRequired || session.ID != "ses_existing" {
		t.Fatalf("session=%+v promptRequired=%v want recovered session with Prompt required", session, promptRequired)
	}
}

func TestEnsureNativeSessionRecoveryCreatesWhenWorkspaceSessionMissing(t *testing.T) {
	var createCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{map[string]any{"id": "ses_other", "directory": "/home/runner/projects/issue"}})
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
		if payload.Location.Directory != hostRunnerWorkspace {
			t.Fatalf("session directory=%q want runner working directory", payload.Location.Directory)
		}
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "ses_new", "directory": hostRunnerWorkspace}})
	})

	native := newWorkspaceTestClient(t, mux, hostRunnerWorkspace)
	session, promptRequired, err := ensureNativeSession(context.Background(), native, executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "test-model"},
	}, settings{ProviderID: "test-provider"}, true)
	if err != nil {
		t.Fatalf("ensureNativeSession() error=%v", err)
	}
	if !promptRequired || session.ID != "ses_new" || session.Directory != hostRunnerWorkspace || createCalls.Load() != 1 {
		t.Fatalf("session=%+v promptRequired=%v createCalls=%d", session, promptRequired, createCalls.Load())
	}
}

func TestNativeWorkingDirectoryUsesProcessPath(t *testing.T) {
	if got := nativeWorkingDirectory(&fakeWorkingDirProcess{dir: hostRunnerWorkspace}); got != hostRunnerWorkspace {
		t.Fatalf("working directory=%q want host runner path", got)
	}
	if got := nativeWorkingDirectory(&fakeWorkingDirProcess{}); got != "/workspace" {
		t.Fatalf("working directory=%q want /workspace fallback", got)
	}
}

type fakeWorkingDirProcess struct {
	fakeOpenCodeProcess
	dir string
}

func (p *fakeWorkingDirProcess) WorkingDirectory() string { return p.dir }

func newWorkspaceTestClient(t *testing.T, handler http.Handler, directory string) *client.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("client.New() error=%v", err)
	}
	native.SetDirectory(directory)
	return native
}
