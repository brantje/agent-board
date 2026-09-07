package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

const (
	Name                         = "opencode"
	defaultAddress               = "127.0.0.1:4096"
	providerCredentialEnv        = "AGENT_BOARD_PROVIDER_API_KEY"
	startupTimeout               = 15 * time.Second
	startupAttemptTimeout        = time.Second
	startupRetryDelay            = 100 * time.Millisecond
	reconnectRetryDelay          = 200 * time.Millisecond
	reconnectAttempts            = 10
	serviceStopTimeout           = 5 * time.Second
)

type Engine struct {
	address string
}

func New() *Engine {
	return &Engine{address: defaultAddress}
}

func newWithAddress(address string) *Engine {
	return &Engine{address: address}
}

func (e *Engine) Name() string { return Name }

func (e *Engine) Execute(ctx context.Context, request engine.Request) (result engine.Result, resultErr error) {
	if request.Launcher == nil {
		return engine.Result{}, fmt.Errorf("opencode engine: process launcher is required")
	}
	if request.InteractiveQuestions == nil {
		return engine.Result{}, fmt.Errorf("opencode engine: interactive Question capability is required")
	}

	settings, err := resolveSettings(request.Context)
	if err != nil {
		return engine.Result{}, err
	}
	env, err := serverEnvironment(request.Context, settings.ProviderID)
	if err != nil {
		return engine.Result{}, err
	}
	process, err := request.Launcher.Start(ctx, engine.ProcessRequest{
		Command: []string{"opencode", "serve", "--hostname", "127.0.0.1", "--port", "4096"},
		CWD:                   "/workspace",
		Env:                   env,
		ProviderCredentialEnv: providerCredentialEnv,
		Kind:                  "tool",
		Name:                  "opencode-server",
	})
	if err != nil {
		return engine.Result{}, fmt.Errorf("opencode engine: start server: %w", err)
	}

	var drainWG sync.WaitGroup
	for _, source := range []io.Reader{process.Stdout(), process.Stderr()} {
		drainWG.Add(1)
		go func(source io.Reader) {
			defer drainWG.Done()
			_, _ = io.Copy(io.Discard, source)
		}(source)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = stopService(ctx, process)
		}
		drainWG.Wait()
	}()

	connector, ok := process.(engine.SessionConnector)
	if !ok {
		return engine.Result{}, fmt.Errorf("opencode engine: process does not support session-local connections")
	}
	native, err := client.NewSession(connector, e.address)
	if err != nil {
		return engine.Result{}, err
	}
	defer native.CloseIdleConnections()
	if err := waitHealthy(ctx, native); err != nil {
		return engine.Result{}, err
	}

	session, err := native.CreateSession(ctx, client.CreateSessionRequest{
		Directory: "/workspace",
		Model: client.ModelRef{
			ID:         request.Context.Model.Model,
			ProviderID: settings.ProviderID,
			Variant:    settings.Variant,
		},
	})
	if err != nil {
		return engine.Result{}, fmt.Errorf("opencode engine: create native session: %w", err)
	}

	stream, err := native.Subscribe(ctx)
	if err != nil {
		return engine.Result{}, fmt.Errorf("opencode engine: subscribe native events: %w", err)
	}
	defer func() { _ = stream.Close() }()
	eventCtx, cancelEventReads := context.WithCancel(ctx)
	defer cancelEventReads()

	state := newRunState(session.ID, request.InteractiveQuestions, activitySink(request.Launcher))
	if err := native.Prompt(ctx, session.ID, initialTaskPrompt(request.Context)); err != nil {
		return engine.Result{}, fmt.Errorf("opencode engine: send initial task: %w", err)
	}

	waitCh := waitSession(ctx, native, session.ID)
	events := readEvents(eventCtx, stream)
	for {
		select {
		case <-ctx.Done():
			cancelNativeSession(ctx, native, session.ID, state)
			return engine.Result{}, ctx.Err()
		case waitErr := <-waitCh:
			if waitErr != nil {
				return engine.Result{}, fmt.Errorf("opencode engine: wait native session: %w", waitErr)
			}
			pending, err := native.ListQuestions(ctx, session.ID)
			if err != nil {
				return engine.Result{}, fmt.Errorf("opencode engine: reconcile pending Questions at idle: %w", err)
			}
			if len(pending) != 0 {
				if err := state.handlePending(ctx, native, pending); err != nil {
					return engine.Result{}, err
				}
				waitCh = waitSession(ctx, native, session.ID)
				continue
			}
			if err := stream.Close(); err != nil {
				return engine.Result{}, fmt.Errorf("opencode engine: close native event stream: %w", err)
			}
			if err := stopService(ctx, process); err != nil {
				return engine.Result{}, err
			}
			stopped = true
			drainWG.Wait()
			if state.activity != nil {
				return engine.Result{}, nil
			}
			return engine.Result{Summary: state.lastVisibleMessage}, nil
		case eventRead := <-events:
			if eventRead.err != nil {
				_ = stream.Close()
				pending, reconcileErr := native.ListQuestions(ctx, session.ID)
				if reconcileErr != nil {
					return engine.Result{}, errors.Join(
						fmt.Errorf("opencode engine: native event stream disconnected: %w", eventRead.err),
						fmt.Errorf("reconcile pending Questions: %w", reconcileErr),
					)
				}
				if err := state.handlePending(ctx, native, pending); err != nil {
					return engine.Result{}, err
				}
				stream, err = reconnectEvents(ctx, native)
				if err != nil {
					return engine.Result{}, errors.Join(fmt.Errorf("opencode engine: native event stream disconnected: %w", eventRead.err), err)
				}
				events = readEvents(eventCtx, stream)
				continue
			}
			if err := state.handleEvent(ctx, native, eventRead.event); err != nil {
				return engine.Result{}, err
			}
		}
	}
}

type settings struct {
	ProviderID string `json:"providerId"`
	Variant    string `json:"variant"`
}

func resolveSettings(safe executioncontext.SafeContext) (settings, error) {
	resolved := settings{ProviderID: strings.TrimSpace(safe.Provider.Kind)}
	if len(safe.Executor.EngineSettings) != 0 {
		var configured settings
		if err := json.Unmarshal(safe.Executor.EngineSettings, &configured); err != nil {
			return settings{}, fmt.Errorf("opencode engine: decode engine settings: %w", err)
		}
		if strings.TrimSpace(configured.ProviderID) != "" {
			resolved.ProviderID = strings.TrimSpace(configured.ProviderID)
		}
		resolved.Variant = strings.TrimSpace(configured.Variant)
	}
	if resolved.ProviderID == "" || strings.TrimSpace(safe.Model.Model) == "" {
		return settings{}, fmt.Errorf("opencode engine: provider id and model are required")
	}
	return resolved, nil
}

func serverEnvironment(safe executioncontext.SafeContext, providerID string) (map[string]string, error) {
	options := map[string]any{
		"apiKey": "{env:" + providerCredentialEnv + "}",
	}
	if safe.Provider.BaseURL != nil && strings.TrimSpace(*safe.Provider.BaseURL) != "" {
		options["baseURL"] = strings.TrimSpace(*safe.Provider.BaseURL)
	}
	config := map[string]any{
		"provider": map[string]any{
			providerID: map[string]any{
				"options": options,
			},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("opencode engine: encode provider config: %w", err)
	}
	return map[string]string{"OPENCODE_CONFIG_CONTENT": string(encoded)}, nil
}

func initialTaskPrompt(safe executioncontext.SafeContext) string {
	var sections []string
	if role := strings.TrimSpace(safe.Agent.RoleInstructions); role != "" {
		sections = append(sections, "Agent role instructions:\n"+role)
	}
	issue := "Issue: " + strings.TrimSpace(safe.Issue.Title)
	if description := strings.TrimSpace(safe.Issue.Description); description != "" {
		issue += "\n\n" + description
	}
	sections = append(sections, issue)
	if safe.ReviewFeedback != nil && strings.TrimSpace(safe.ReviewFeedback.Feedback) != "" {
		sections = append(sections, "Review feedback:\n"+strings.TrimSpace(safe.ReviewFeedback.Feedback))
	}
	sections = append(sections, "Work directly in /workspace and implement the requested issue. If human input is required, use OpenCode's native Question capability rather than guessing.")
	return strings.Join(sections, "\n\n")
}

func activitySink(launcher engine.ProcessLauncher) engine.ActivitySink {
	activity, _ := launcher.(engine.ActivitySink)
	return activity
}

func waitHealthy(parent context.Context, native *client.Client) error {
	ctx, cancel := context.WithTimeout(parent, startupTimeout)
	defer cancel()
	var lastErr error
	for {
		attemptCtx, attemptCancel := context.WithTimeout(ctx, startupAttemptTimeout)
		_, err := native.Health(attemptCtx)
		attemptCancel()
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return parent.Err()
			}
			return fmt.Errorf("opencode engine: native server did not become healthy: %w", lastErr)
		case <-time.After(startupRetryDelay):
		}
	}
}

func reconnectEvents(ctx context.Context, native *client.Client) (*client.EventStream, error) {
	var lastErr error
	for attempt := 0; attempt < reconnectAttempts; attempt++ {
		stream, err := native.Subscribe(ctx)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(reconnectRetryDelay):
		}
	}
	return nil, fmt.Errorf("opencode engine: reconnect native event stream: %w", lastErr)
}

func waitSession(ctx context.Context, native *client.Client, sessionID string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- native.WaitSession(ctx, sessionID) }()
	return done
}

type eventReadResult struct {
	event client.Event
	err   error
}

func readEvents(ctx context.Context, stream *client.EventStream) <-chan eventReadResult {
	reads := make(chan eventReadResult, 1)
	go func() {
		for {
			event, err := stream.Next()
			select {
			case reads <- eventReadResult{event: event, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return reads
}

func cancelNativeSession(parent context.Context, native *client.Client, sessionID string, state *runState) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), serviceStopTimeout)
	defer cancel()
	for requestID := range state.activeNativeRequests() {
		_ = native.RejectQuestion(ctx, sessionID, requestID)
	}
	_ = native.InterruptSession(ctx, sessionID)
}

func stopService(parent context.Context, process engine.Process) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), serviceStopTimeout)
	defer cancel()
	_ = process.Terminate(ctx)
	result, err := process.Wait(ctx)
	if err == nil {
		_ = result
		return nil
	}
	_ = process.Kill(ctx)
	_, waitErr := process.Wait(ctx)
	if waitErr != nil {
		return fmt.Errorf("opencode engine: stop native server: %w", errors.Join(err, waitErr))
	}
	return nil
}

var _ engine.Engine = (*Engine)(nil)
