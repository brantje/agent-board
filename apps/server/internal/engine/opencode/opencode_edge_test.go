package opencode

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestEngineMetadataAndRequiredCapabilities(t *testing.T) {
	adapter := New()
	if adapter.Name() != Name {
		t.Fatalf("Name()=%q want %q", adapter.Name(), Name)
	}
	if _, err := adapter.Execute(context.Background(), engine.Request{}); err == nil || !strings.Contains(err.Error(), "process launcher is required") {
		t.Fatalf("Execute() without launcher error=%v", err)
	}
	if _, err := adapter.Execute(context.Background(), engine.Request{Launcher: &fakeOpenCodeLauncher{}}); err == nil || !strings.Contains(err.Error(), "interactive Question capability is required") {
		t.Fatalf("Execute() without interactive Questions error=%v", err)
	}
}

func TestResolveSettingsUsesProviderDefaultsAndEngineOverrides(t *testing.T) {
	base := executioncontext.SafeContext{
		Provider: executioncontext.ProviderContext{Kind: "openrouter"},
		Model:    executioncontext.ModelContext{Model: "anthropic/claude-sonnet"},
	}
	resolved, err := resolveSettings(base)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProviderID != "openrouter" || resolved.Variant != "" {
		t.Fatalf("settings=%+v", resolved)
	}

	base.Agent.EngineSettings = []byte(`{"providerId":"custom-openrouter","variant":"high"}`)
	resolved, err = resolveSettings(base)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProviderID != "custom-openrouter" || resolved.Variant != "high" {
		t.Fatalf("override settings=%+v", resolved)
	}

	base.Agent.EngineSettings = []byte(`{`)
	if _, err := resolveSettings(base); err == nil || !strings.Contains(err.Error(), "decode engine settings") {
		t.Fatalf("malformed settings error=%v", err)
	}
	base.Agent.EngineSettings = nil
	base.Provider.Kind = " "
	if _, err := resolveSettings(base); err == nil || !strings.Contains(err.Error(), "provider id and model are required") {
		t.Fatalf("missing provider error=%v", err)
	}
	base.Provider.Kind = "openrouter"
	base.Model.Model = " "
	if _, err := resolveSettings(base); err == nil || !strings.Contains(err.Error(), "provider id and model are required") {
		t.Fatalf("missing model error=%v", err)
	}
}

func TestInitialTaskPromptIncludesReviewFeedback(t *testing.T) {
	feedback := executioncontext.ReviewFeedbackContext{Feedback: "keep the public API stable"}
	prompt := initialTaskPrompt(executioncontext.SafeContext{
		Issue:          executioncontext.IssueContext{Title: "Refine adapter", Description: "Preserve behavior."},
		Agent:          executioncontext.AgentContext{RoleInstructions: "Prefer maintainable code."},
		ReviewFeedback: &feedback,
	})
	for _, want := range []string{"Prefer maintainable code.", "Refine adapter", "Preserve behavior.", "keep the public API stable", "native Question capability"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
}

func TestWaitHealthyRetriesAndHonorsParentCancellation(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		writeNativeJSON(t, w, map[string]any{"healthy": true, "version": "test"})
	}))
	defer server.Close()
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitHealthy(context.Background(), native); err != nil {
		t.Fatalf("waitHealthy() error=%v", err)
	}
	if attempts.Load() < 2 {
		t.Fatalf("health attempts=%d want retry", attempts.Load())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitHealthy(ctx, native); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waitHealthy() error=%v", err)
	}
}

func TestReconnectEventsHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reconnectEvents(ctx, native); !errors.Is(err, context.Canceled) {
		t.Fatalf("reconnectEvents() error=%v", err)
	}
}

type lifecycleProcess struct {
	waitErrors     []error
	waitCalls      int
	terminateCalls int
	killCalls      int
}

func (p *lifecycleProcess) ID() string            { return "process-1" }
func (p *lifecycleProcess) Stdout() io.Reader     { return strings.NewReader("") }
func (p *lifecycleProcess) Stderr() io.Reader     { return strings.NewReader("") }
func (p *lifecycleProcess) Stdin() io.WriteCloser { return nopWriteCloser{Writer: io.Discard} }
func (p *lifecycleProcess) Terminate(context.Context) error {
	p.terminateCalls++
	return nil
}
func (p *lifecycleProcess) Kill(context.Context) error {
	p.killCalls++
	return nil
}
func (p *lifecycleProcess) Wait(context.Context) (engine.ProcessResult, error) {
	p.waitCalls++
	index := p.waitCalls - 1
	if index < len(p.waitErrors) && p.waitErrors[index] != nil {
		return engine.ProcessResult{}, p.waitErrors[index]
	}
	return engine.ProcessResult{ExitCode: 0}, nil
}

func TestStopServiceHandlesImmediateProcessResult(t *testing.T) {
	clean := &lifecycleProcess{}
	if err := stopService(context.Background(), clean); err != nil {
		t.Fatalf("clean stop error=%v", err)
	}
	if clean.terminateCalls != 1 || clean.killCalls != 0 || clean.waitCalls != 1 {
		t.Fatalf("clean lifecycle terminate=%d kill=%d wait=%d", clean.terminateCalls, clean.killCalls, clean.waitCalls)
	}

	waitErr := errors.New("wait failed")
	failed := &lifecycleProcess{waitErrors: []error{waitErr}}
	if err := stopService(context.Background(), failed); err == nil || !strings.Contains(err.Error(), "stop native server") {
		t.Fatalf("failed stop error=%v", err)
	}
	if failed.terminateCalls != 1 || failed.killCalls != 0 || failed.waitCalls != 1 {
		t.Fatalf("failed lifecycle terminate=%d kill=%d wait=%d", failed.terminateCalls, failed.killCalls, failed.waitCalls)
	}
}

var _ engine.Process = (*lifecycleProcess)(nil)
