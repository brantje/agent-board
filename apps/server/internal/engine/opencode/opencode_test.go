package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

type fakeOpenCodeProcess struct {
	target string
	done   chan struct{}
	once   sync.Once
}

func newFakeOpenCodeProcess(target string) *fakeOpenCodeProcess {
	return &fakeOpenCodeProcess{target: target, done: make(chan struct{})}
}

func (p *fakeOpenCodeProcess) ID() string            { return "execution-session-1" }
func (p *fakeOpenCodeProcess) Stdout() io.Reader     { return strings.NewReader("") }
func (p *fakeOpenCodeProcess) Stderr() io.Reader     { return strings.NewReader("") }
func (p *fakeOpenCodeProcess) Stdin() io.WriteCloser { return nopWriteCloser{Writer: &bytes.Buffer{}} }
func (p *fakeOpenCodeProcess) Wait(ctx context.Context) (engine.ProcessResult, error) {
	select {
	case <-p.done:
		return engine.ProcessResult{ExitCode: 0}, nil
	case <-ctx.Done():
		return engine.ProcessResult{}, ctx.Err()
	}
}
func (p *fakeOpenCodeProcess) Terminate(context.Context) error { p.once.Do(func() { close(p.done) }); return nil }
func (p *fakeOpenCodeProcess) Kill(context.Context) error      { p.once.Do(func() { close(p.done) }); return nil }
func (p *fakeOpenCodeProcess) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, p.target)
}

type nopWriteCloser struct{ io.Writer }
func (nopWriteCloser) Close() error { return nil }

type fakeOpenCodeLauncher struct {
	process *fakeOpenCodeProcess
	request engine.ProcessRequest
	starts  int
}

func (l *fakeOpenCodeLauncher) Start(_ context.Context, request engine.ProcessRequest) (engine.Process, error) {
	l.starts++
	l.request = request
	return l.process, nil
}

type fakeInteractiveQuestions struct {
	mu           sync.Mutex
	opened       []string
	correlations []string
	answers      map[string]engine.QuestionAnswer
	resolved     []string
}

func (q *fakeInteractiveQuestions) Open(_ context.Context, correlation string, request engine.QuestionRequest) (engine.Question, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	id := "canonical-" + strings.ReplaceAll(correlation, "/", "-")
	q.opened = append(q.opened, id)
	q.correlations = append(q.correlations, correlation)
	if q.answers == nil {
		q.answers = make(map[string]engine.QuestionAnswer)
	}
	if request.Kind == "TEXT" {
		text := "because it is safer"
		q.answers[id] = engine.QuestionAnswer{Kind: "TEXT", Text: &text}
	} else {
		q.answers[id] = engine.QuestionAnswer{Kind: request.Kind, OptionIDs: []string{"option-1"}}
	}
	return engine.Question{ID: id, Blocking: true}, nil
}

func (q *fakeInteractiveQuestions) WaitAnswer(_ context.Context, questionID string) (engine.QuestionAnswer, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.answers[questionID], nil
}

func (q *fakeInteractiveQuestions) Resolve(_ context.Context, questionID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.resolved = append(q.resolved, questionID)
	return nil
}

type nativeServerHarness struct {
	mu              sync.Mutex
	promptCalls     int
	promptText      string
	modelProvider   string
	modelID         string
	location        string
	replyAnswers    [][]string
	replied         bool
	questionRequest map[string]any
	events          chan string
	replyDone       chan struct{}
	closeReply      sync.Once
}

func newNativeServerHarness() *nativeServerHarness {
	return &nativeServerHarness{
		events:    make(chan string, 8),
		replyDone: make(chan struct{}),
		questionRequest: map[string]any{
			"id": "que_native", "sessionID": "ses_native", "questions": []any{
				map[string]any{
					"question": "Which implementation?", "header": "Implementation",
					"options": []any{
						map[string]any{"label": "A", "description": "first"},
						map[string]any{"label": "B", "description": "second"},
					},
				},
				map[string]any{"question": "Why?", "header": "Reason", "options": []any{}},
			},
		},
	}
}

func (h *nativeServerHarness) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model struct {
				ID         string `json:"id"`
				ProviderID string `json:"providerID"`
			} `json:"model"`
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode session: %v", err)
		}
		h.mu.Lock()
		h.modelProvider, h.modelID, h.location = payload.Model.ProviderID, payload.Model.ID, payload.Location.Directory
		h.mu.Unlock()
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "ses_native"}})
	})
	mux.HandleFunc("GET /api/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("response writer does not flush")
			return
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"connected\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-h.events:
				_, _ = io.WriteString(w, "data: "+event+"\n\n")
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("POST /api/session/ses_native/prompt", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Prompt struct{ Text string `json:"text"` } `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode prompt: %v", err)
		}
		h.mu.Lock()
		h.promptCalls++
		h.promptText = payload.Prompt.Text
		h.mu.Unlock()
		question, _ := json.Marshal(h.questionRequest)
		event, _ := json.Marshal(map[string]any{"id": "evt_question", "type": "question.v2.asked", "properties": json.RawMessage(question)})
		h.events <- string(event)
		writeNativeJSON(t, w, map[string]any{"data": map[string]any{"id": "input_native"}})
	})
	mux.HandleFunc("POST /api/session/ses_native/wait", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-h.replyDone:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("GET /api/session/ses_native/question", func(w http.ResponseWriter, _ *http.Request) {
		h.mu.Lock()
		replied := h.replied
		h.mu.Unlock()
		if replied {
			writeNativeJSON(t, w, map[string]any{"data": []any{}})
			return
		}
		writeNativeJSON(t, w, map[string]any{"data": []any{h.questionRequest}})
	})
	mux.HandleFunc("POST /api/session/ses_native/question/que_native/reply", func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ Answers [][]string `json:"answers"` }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode native answer: %v", err)
		}
		h.mu.Lock()
		h.replyAnswers = payload.Answers
		h.replied = true
		h.mu.Unlock()
		h.closeReply.Do(func() { close(h.replyDone) })
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/session/ses_native/interrupt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /api/session/ses_native/question/que_native/reject", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux
}

func TestEngineAnswersNativeQuestionWithoutSecondPrompt(t *testing.T) {
	harness := newNativeServerHarness()
	server := httptest.NewServer(harness.handler(t))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	process := newFakeOpenCodeProcess(parsed.Host)
	launcher := &fakeOpenCodeLauncher{process: process}
	questions := &fakeInteractiveQuestions{}
	baseURL := "https://provider.example/v1"
	adapter := newWithAddress(parsed.Host)

	_, err = adapter.Execute(context.Background(), engine.Request{
		Context: executioncontext.SafeContext{
			Issue:    executioncontext.IssueContext{Title: "Implement native adapter", Description: "Keep the same native session alive."},
			Agent:    executioncontext.AgentContext{RoleInstructions: "Make maintainable changes."},
			Executor: executioncontext.ExecutorContext{Engine: Name},
			Model:    executioncontext.ModelContext{Model: "claude-sonnet"},
			Provider: executioncontext.ProviderContext{Kind: "anthropic", BaseURL: &baseURL},
		},
		Launcher:             launcher,
		InteractiveQuestions: questions,
	})
	if err != nil {
		t.Fatalf("Execute() error=%v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if launcher.starts != 1 {
		t.Fatalf("server starts=%d", launcher.starts)
	}
	if harness.promptCalls != 1 {
		t.Fatalf("prompt calls=%d want 1", harness.promptCalls)
	}
	if harness.modelProvider != "anthropic" || harness.modelID != "claude-sonnet" || harness.location != "/workspace" {
		t.Fatalf("model=%s/%s location=%q", harness.modelProvider, harness.modelID, harness.location)
	}
	if len(harness.replyAnswers) != 2 || len(harness.replyAnswers[0]) != 1 || harness.replyAnswers[0][0] != "B" || len(harness.replyAnswers[1]) != 1 || harness.replyAnswers[1][0] != "because it is safer" {
		t.Fatalf("native answers=%v", harness.replyAnswers)
	}
	if len(questions.opened) != 2 || len(questions.resolved) != 2 {
		t.Fatalf("opened=%v resolved=%v", questions.opened, questions.resolved)
	}
	if questions.correlations[0] != "ses_native/que_native/0" || questions.correlations[1] != "ses_native/que_native/1" {
		t.Fatalf("correlations=%v", questions.correlations)
	}
	if launcher.request.ProviderCredentialEnv != providerCredentialEnv {
		t.Fatalf("credential env=%q", launcher.request.ProviderCredentialEnv)
	}
	config := launcher.request.Env["OPENCODE_CONFIG_CONTENT"]
	if strings.Contains(config, "secret") || !strings.Contains(config, "{env:"+providerCredentialEnv+"}") || !strings.Contains(config, baseURL) {
		t.Fatalf("unexpected OpenCode provider config=%q", config)
	}
	if !strings.Contains(harness.promptText, "Implement native adapter") || strings.Contains(harness.promptText, "user answered") {
		t.Fatalf("initial prompt=%q", harness.promptText)
	}
}

func writeNativeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

var _ engine.Process = (*fakeOpenCodeProcess)(nil)
var _ engine.SessionConnector = (*fakeOpenCodeProcess)(nil)
var _ engine.InteractiveQuestioner = (*fakeInteractiveQuestions)(nil)
