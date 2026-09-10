package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

type fakeAttachLauncher struct {
	fakeOpenCodeLauncher
	attaches int
}

func (l *fakeAttachLauncher) Attach(context.Context) (engine.Process, error) {
	l.attaches++
	return l.process, nil
}

func TestEngineAttachDoesNotPromptExistingNativeSession(t *testing.T) {
	var statusCalls atomic.Int32
	var promptCalls atomic.Int32
	var createCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{map[string]any{"id": "ses_existing"}})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		call := statusCalls.Add(1)
		if call <= 2 {
			writeNativeJSON(t, w, map[string]any{"ses_existing": map[string]any{"type": "busy"}})
			return
		}
		writeNativeJSON(t, w, map[string]any{"ses_existing": map[string]any{"type": "idle"}})
	})
	mux.HandleFunc("GET /question", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{})
	})
	mux.HandleFunc("GET /api/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"id\":\"connected\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, _ *http.Request) {
		createCalls.Add(1)
		http.Error(w, "should not create", http.StatusInternalServerError)
	})
	mux.HandleFunc("POST /session/ses_existing/prompt_async", func(w http.ResponseWriter, _ *http.Request) {
		promptCalls.Add(1)
		http.Error(w, "should not prompt", http.StatusInternalServerError)
	})
	mux.HandleFunc("POST /api/session/ses_existing/prompt", func(w http.ResponseWriter, _ *http.Request) {
		promptCalls.Add(1)
		http.Error(w, "should not prompt", http.StatusInternalServerError)
	})
	mux.HandleFunc("POST /session/ses_existing/abort", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	result := executeAttached(t, mux)
	if result.launcher.starts != 0 || result.launcher.attaches != 1 {
		t.Fatalf("starts=%d attaches=%d", result.launcher.starts, result.launcher.attaches)
	}
	if promptCalls.Load() != 0 || createCalls.Load() != 0 {
		t.Fatalf("promptCalls=%d createCalls=%d", promptCalls.Load(), createCalls.Load())
	}
}

func TestEngineAttachOpensPendingQuestions(t *testing.T) {
	var replied atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{map[string]any{"id": "ses_existing"}})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		if replied.Load() {
			writeNativeJSON(t, w, map[string]any{"ses_existing": map[string]any{"type": "idle"}})
			return
		}
		writeNativeJSON(t, w, map[string]any{"ses_existing": map[string]any{"type": "busy"}})
	})
	mux.HandleFunc("GET /question", func(w http.ResponseWriter, _ *http.Request) {
		if replied.Load() {
			writeNativeJSON(t, w, []any{})
			return
		}
		writeNativeJSON(t, w, []any{
			map[string]any{
				"id": "que_pending", "sessionID": "ses_existing", "questions": []any{
					map[string]any{
						"question": "Which stack?", "header": "Stack",
						"options": []any{map[string]any{"label": "A", "description": "first"}, map[string]any{"label": "B", "description": "second"}},
					},
				},
			},
		})
	})
	mux.HandleFunc("POST /question/que_pending/reply", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Answers [][]string `json:"answers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode reply: %v", err)
		}
		replied.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"id\":\"connected\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("POST /session/ses_existing/prompt_async", func(http.ResponseWriter, *http.Request) {
		t.Error("attach must not prompt existing native session")
	})
	mux.HandleFunc("POST /session/ses_existing/abort", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	result := executeAttached(t, mux)
	if result.launcher.starts != 0 || result.launcher.attaches != 1 {
		t.Fatalf("starts=%d attaches=%d", result.launcher.starts, result.launcher.attaches)
	}
	if len(result.questions.opened) != 1 || len(result.questions.resolved) != 1 {
		t.Fatalf("opened=%v resolved=%v", result.questions.opened, result.questions.resolved)
	}
}

func TestEngineAttachPromptsWhenNoNativeSession(t *testing.T) {
	var promptCalls atomic.Int32
	var statusCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{})
	})
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"id": "ses_new"})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("legacy session creation must not fall back to V2")
		http.Error(w, "unexpected V2 create", http.StatusInternalServerError)
	})
	mux.HandleFunc("POST /session/ses_new/prompt_async", func(w http.ResponseWriter, _ *http.Request) {
		promptCalls.Add(1)
		writeNativeJSON(t, w, true)
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		if promptCalls.Load() == 0 {
			writeNativeJSON(t, w, map[string]any{})
			return
		}
		if statusCalls.Add(1) <= 2 {
			writeNativeJSON(t, w, map[string]any{"ses_new": map[string]any{"type": "busy"}})
			return
		}
		writeNativeJSON(t, w, map[string]any{})
	})
	mux.HandleFunc("GET /question", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{})
	})
	mux.HandleFunc("GET /api/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"id\":\"connected\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("POST /session/ses_new/abort", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	result := executeAttached(t, mux)
	if result.launcher.starts != 0 || result.launcher.attaches != 1 {
		t.Fatalf("starts=%d attaches=%d", result.launcher.starts, result.launcher.attaches)
	}
	if promptCalls.Load() != 1 {
		t.Fatalf("promptCalls=%d want 1", promptCalls.Load())
	}
}

func TestEngineAttachUsesIdleNativeSessionWithoutPrompt(t *testing.T) {
	var promptCalls atomic.Int32
	var createCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{map[string]any{"id": ""}, map[string]any{"id": "ses_idle"}})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"ses_idle": map[string]any{"type": "idle"}})
	})
	mux.HandleFunc("GET /question", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{})
	})
	mux.HandleFunc("GET /api/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"id\":\"connected\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, _ *http.Request) {
		createCalls.Add(1)
		http.Error(w, "should not create", http.StatusInternalServerError)
	})
	mux.HandleFunc("POST /session/ses_idle/prompt_async", func(w http.ResponseWriter, _ *http.Request) {
		promptCalls.Add(1)
		http.Error(w, "should not prompt", http.StatusInternalServerError)
	})
	mux.HandleFunc("POST /session/ses_idle/abort", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	result := executeAttached(t, mux)
	if result.launcher.starts != 0 || result.launcher.attaches != 1 {
		t.Fatalf("starts=%d attaches=%d", result.launcher.starts, result.launcher.attaches)
	}
	if promptCalls.Load() != 0 || createCalls.Load() != 0 {
		t.Fatalf("promptCalls=%d createCalls=%d", promptCalls.Load(), createCalls.Load())
	}
}

func TestLaunchOpenCodeProcessStartsWhenAttachIsNotApplicable(t *testing.T) {
	launcher := &notAttachableLauncher{fakeOpenCodeLauncher: fakeOpenCodeLauncher{process: newFakeOpenCodeProcess("127.0.0.1:1")}}
	process, recovered, err := launchOpenCodeProcess(context.Background(), launcher, "127.0.0.1", "4096", nil)
	if err != nil {
		t.Fatalf("launchOpenCodeProcess() error=%v", err)
	}
	if recovered {
		t.Fatal("fresh start must not be treated as recovered attach")
	}
	if launcher.starts != 1 {
		t.Fatalf("starts=%d want 1", launcher.starts)
	}
	if process == nil {
		t.Fatal("expected started process")
	}
}

func TestLaunchOpenCodeProcessRejectsNilAttachedProcess(t *testing.T) {
	_, _, err := launchOpenCodeProcess(context.Background(), &nilAttachLauncher{}, "127.0.0.1", "4096", nil)
	if err == nil || !strings.Contains(err.Error(), "attached process is unavailable") {
		t.Fatalf("launchOpenCodeProcess() error=%v", err)
	}
}

func TestEngineAttachReportsLauncherFailure(t *testing.T) {
	adapter := newWithAddress("127.0.0.1:4096")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	_, err := adapter.Execute(ctx, engine.Request{
		Context: executioncontext.SafeContext{
			Issue:    executioncontext.IssueContext{Title: "Resume after restart"},
			Agent:    executioncontext.AgentContext{Engine: Name},
			Model:    executioncontext.ModelContext{Model: "test-model"},
			Provider: executioncontext.ProviderContext{Kind: "test-provider"},
		},
		Launcher:             &failingAttachLauncher{},
		InteractiveQuestions: &fakeInteractiveQuestions{},
	})
	if err == nil || !strings.Contains(err.Error(), "attach existing server") {
		t.Fatalf("Execute() error=%v", err)
	}
}

func TestEngineAttachFailsWhenNativeSessionListFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "list failed", http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "list failed", http.StatusInternalServerError)
	})
	err := executeAttachedError(t, mux)
	if err == nil || !strings.Contains(err.Error(), "list native sessions") {
		t.Fatalf("Execute() error=%v", err)
	}
}

func TestEngineAttachFailsWhenNativeStatusFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, []any{map[string]any{"id": "ses_existing"}})
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "status failed", http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /api/session/active", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "status failed", http.StatusInternalServerError)
	})
	err := executeAttachedError(t, mux)
	if err == nil || !strings.Contains(err.Error(), "query native session") {
		t.Fatalf("Execute() error=%v", err)
	}
}

type notAttachableLauncher struct {
	fakeOpenCodeLauncher
}

func (l *notAttachableLauncher) Attach(context.Context) (engine.Process, error) {
	return nil, engine.ErrNotAttachable
}

type nilAttachLauncher struct {
	fakeOpenCodeLauncher
}

func (l *nilAttachLauncher) Attach(context.Context) (engine.Process, error) {
	return nil, nil
}

type failingAttachLauncher struct{}

func (failingAttachLauncher) Start(context.Context, engine.ProcessRequest) (engine.Process, error) {
	return nil, errors.New("start should not be called")
}

func (failingAttachLauncher) Attach(context.Context) (engine.Process, error) {
	return nil, errors.New("runner unavailable")
}

type attachedExecuteResult struct {
	launcher  *fakeAttachLauncher
	questions *fakeInteractiveQuestions
}

func executeAttached(t *testing.T, mux http.Handler) attachedExecuteResult {
	t.Helper()
	launcher, questions, err := executeAttachedRequest(t, mux)
	if err != nil {
		t.Fatalf("Execute() error=%v", err)
	}
	return attachedExecuteResult{launcher: launcher, questions: questions}
}

func executeAttachedError(t *testing.T, mux http.Handler) error {
	t.Helper()
	_, _, err := executeAttachedRequest(t, mux)
	return err
}

func executeAttachedRequest(t *testing.T, mux http.Handler) (*fakeAttachLauncher, *fakeInteractiveQuestions, error) {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	process := newFakeOpenCodeProcess(parsed.Host)
	launcher := &fakeAttachLauncher{fakeOpenCodeLauncher: fakeOpenCodeLauncher{process: process}}
	questions := &fakeInteractiveQuestions{}
	adapter := newWithAddress(parsed.Host)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	_, err = adapter.Execute(ctx, engine.Request{
		Context: executioncontext.SafeContext{
			Issue:    executioncontext.IssueContext{Title: "Resume after restart"},
			Agent:    executioncontext.AgentContext{Engine: Name},
			Model:    executioncontext.ModelContext{Model: "test-model"},
			Provider: executioncontext.ProviderContext{Kind: "test-provider"},
		},
		Launcher:             launcher,
		InteractiveQuestions: questions,
	})
	return launcher, questions, err
}
