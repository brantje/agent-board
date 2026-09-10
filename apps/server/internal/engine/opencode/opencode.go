package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

const (
	Name                        = "opencode"
	defaultAddress              = "127.0.0.1:4096"
	providerCredentialEnv       = "AGENT_BOARD_PROVIDER_API_KEY"
	startupTimeout              = 15 * time.Second
	startupAttemptTimeout       = time.Second
	startupRetryDelay           = 100 * time.Millisecond
	reconnectRetryDelay         = 200 * time.Millisecond
	reconnectAttempts           = 10
	nativeStatePollInterval     = 250 * time.Millisecond
	nativeStatePollFailureLimit = 3
	inactivePollsBeforeComplete = 2
	promptAdmissionPollLimit    = 40
	serviceTerminateGrace       = time.Second
	serviceStopTimeout          = 5 * time.Second
)

var builtInOpenCodeProviders = map[string]struct{}{
	"amazon-bedrock": {},
	"anthropic":      {},
	"azure":          {},
	"cerebras":       {},
	"deepseek":       {},
	"fireworks":      {},
	"github-copilot": {},
	"gitlab":         {},
	"google":         {},
	"google-vertex":  {},
	"groq":           {},
	"mistral":        {},
	"openai":         {},
	"openrouter":     {},
	"opencode":       {},
	"perplexity":     {},
	"together":       {},
	"xai":            {},
}

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
	host, port, err := net.SplitHostPort(e.address)
	if err != nil {
		return engine.Result{}, fmt.Errorf("opencode engine: parse native server address: %w", err)
	}
	process, recovered, err := launchOpenCodeProcess(ctx, request.Launcher, host, port, env)
	if err != nil {
		return engine.Result{}, err
	}

	var drainWG sync.WaitGroup
	for _, source := range []io.Reader{process.Stdout(), process.Stderr()} {
		drainWG.Add(1)
		go func(source io.Reader) {
			defer drainWG.Done()
			_, _ = io.Copy(io.Discard, source)
		}(source)
	}
	drainDone := make(chan struct{})
	go func() {
		drainWG.Wait()
		close(drainDone)
	}()

	stopped := false
	defer func() {
		if !stopped {
			stopped = true
			_ = stopService(ctx, process)
		}
		waitDrained(drainDone, serviceStopTimeout)
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

	session, promptRequired, err := ensureNativeSession(ctx, native, request.Context, settings, recovered)
	if err != nil {
		return engine.Result{}, err
	}

	stream, err := native.Subscribe(ctx)
	if err != nil {
		return engine.Result{}, fmt.Errorf("opencode engine: subscribe native events: %w", err)
	}
	defer func() { _ = stream.Close() }()
	eventCtx, cancelEventReads := context.WithCancel(ctx)
	defer cancelEventReads()

	state := newRunState(session.ID, request.InteractiveQuestions, activitySink(request.Launcher))
	state.seedModelUsage(settings.ProviderID, request.Context.Model.Model, nil)
	if limit, err := native.ModelContextLimit(ctx, settings.ProviderID, request.Context.Model.Model); err == nil {
		state.contextLimitTokens = cloneInt64(limit)
	}
	if err := state.backfillUsageFromHistory(ctx, native); err != nil {
		return engine.Result{}, err
	}
	if promptRequired {
		if err := native.Prompt(ctx, session.ID, initialTaskPrompt(request.Context)); err != nil {
			return engine.Result{}, fmt.Errorf("opencode engine: send initial task: %w", err)
		}
	}

	finish := func() (engine.Result, error) {
		_ = stream.Close()
		stopped = true
		if err := stopService(ctx, process); err != nil {
			waitDrained(drainDone, serviceStopTimeout)
			return engine.Result{}, err
		}
		waitDrained(drainDone, serviceStopTimeout)
		if state.activity != nil {
			return engine.Result{}, nil
		}
		return engine.Result{Summary: state.lastVisibleMessage}, nil
	}
	finishCompleted := func() (engine.Result, error) {
		if err := state.flushPendingMessages(ctx); err != nil {
			return engine.Result{}, err
		}
		message, failed, err := native.LatestAssistantError(ctx, session.ID)
		if err != nil {
			return engine.Result{}, fmt.Errorf("opencode engine: inspect native assistant completion: %w", err)
		}
		if failed {
			return engine.Result{}, fmt.Errorf("opencode engine: native session error: %s", message)
		}
		return finish()
	}

	events := readEvents(eventCtx, stream)
	statePoll := time.NewTicker(nativeStatePollInterval)
	defer statePoll.Stop()
	executionObserved := recovered && !promptRequired
	inactivePolls := 0
	promptStallPolls := 0
	statePollFailures := 0
	for {
		select {
		case <-ctx.Done():
			cancelNativeSession(ctx, native, session.ID, state)
			return engine.Result{}, ctx.Err()
		case <-statePoll.C:
			hadPending, err := reconcilePendingQuestionsForPoll(ctx, native, session.ID, state)
			if err != nil {
				return engine.Result{}, err
			}
			if hadPending {
				executionObserved = true
				inactivePolls = 0
				continue
			}
			active, err := queryNativeSessionActive(ctx, native, session.ID)
			if err != nil {
				if isTransientNativePollTimeout(ctx, err) {
					continue
				}
				statePollFailures++
				inactivePolls = 0
				if statePollFailures >= nativeStatePollFailureLimit {
					return engine.Result{}, fmt.Errorf("opencode engine: query native session activity after %d consecutive failures: %w", statePollFailures, err)
				}
				continue
			}
			statePollFailures = 0
			if active {
				executionObserved = true
				promptStallPolls = 0
				inactivePolls = 0
				continue
			}
			// Prompt admission and execution ownership are asynchronous in OpenCode.
			// A newly admitted session can therefore be absent from /session/status
			// briefly before its drain starts. Treat inactivity as completion only after
			// this exact session has been observed running (or asking a Question).
			if !executionObserved {
				promptStallPolls++
				if promptStallPolls >= promptAdmissionPollLimit {
					detail := nativeSessionStallDetail(ctx, native, session.ID)
					return engine.Result{}, fmt.Errorf("opencode engine: native session did not start execution after prompt%s", detail)
				}
				inactivePolls = 0
				continue
			}
			inactivePolls++
			if inactivePolls >= inactivePollsBeforeComplete {
				return finishCompleted()
			}
		case eventRead := <-events:
			if eventRead.err != nil {
				_ = stream.Close()
				if _, reconcileErr := reconcilePendingQuestions(ctx, native, session.ID, state); reconcileErr != nil {
					return engine.Result{}, errors.Join(
						fmt.Errorf("opencode engine: native event stream disconnected: %w", eventRead.err),
						reconcileErr,
					)
				}
				stream, err = reconnectEvents(ctx, native)
				if err != nil {
					return engine.Result{}, errors.Join(fmt.Errorf("opencode engine: native event stream disconnected: %w", eventRead.err), err)
				}
				events = readEvents(eventCtx, stream)
				continue
			}
			idle, err := isSessionIdleEvent(eventRead.event, session.ID)
			if err != nil {
				return engine.Result{}, err
			}
			if idle {
				if !executionObserved {
					active, err := queryNativeSessionActive(ctx, native, session.ID)
					if err != nil {
						if !isTransientNativePollTimeout(ctx, err) {
							return engine.Result{}, fmt.Errorf("opencode engine: query native session activity before idle handling: %w", err)
						}
					} else if !active {
						return engine.Result{}, fmt.Errorf("opencode engine: native session became idle before execution started")
					} else {
						executionObserved = true
						promptStallPolls = 0
						inactivePolls = 0
					}
				}
				hadPending, err := reconcilePendingQuestionsForPoll(ctx, native, session.ID, state)
				if err != nil {
					return engine.Result{}, err
				}
				if !hadPending {
					return finishCompleted()
				}
				continue
			}
			if err := state.handleEvent(ctx, native, eventRead.event); err != nil {
				return engine.Result{}, err
			}
			if indicatesSessionExecution(eventRead.event, session.ID) || len(state.nativeQuestions) > 0 {
				executionObserved = true
				inactivePolls = 0
				promptStallPolls = 0
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
	if len(safe.Agent.EngineSettings) != 0 {
		var configured settings
		if err := json.Unmarshal(safe.Agent.EngineSettings, &configured); err != nil {
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

func isBuiltInOpenCodeProvider(providerID string) bool {
	_, ok := builtInOpenCodeProviders[strings.TrimSpace(providerID)]
	return ok
}

func serverEnvironment(safe executioncontext.SafeContext, providerID string) (map[string]string, error) {
	modelID := strings.TrimSpace(safe.Model.Model)
	options := map[string]any{
		"apiKey": "{env:" + providerCredentialEnv + "}",
	}
	if safe.Provider.BaseURL != nil && strings.TrimSpace(*safe.Provider.BaseURL) != "" {
		options["baseURL"] = strings.TrimSpace(*safe.Provider.BaseURL)
	}
	providerConfig := map[string]any{
		"options": options,
		"models": map[string]any{
			modelID: map[string]any{
				"name": modelID,
			},
		},
	}
	if !isBuiltInOpenCodeProvider(providerID) {
		providerConfig["npm"] = "@ai-sdk/openai-compatible"
		if name := strings.TrimSpace(safe.Provider.Name); name != "" {
			providerConfig["name"] = name
		}
	}
	config := map[string]any{
		"provider": map[string]any{
			providerID: providerConfig,
		},
	}
	if providerID != "" && modelID != "" {
		ref := providerID + "/" + modelID
		config["model"] = ref
		config["small_model"] = ref
		config["agent"] = map[string]any{
			"title": map[string]any{"disable": true},
		}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("opencode engine: encode provider config: %w", err)
	}
	return map[string]string{"OPENCODE_CONFIG_CONTENT": string(encoded)}, nil
}

func launchOpenCodeProcess(ctx context.Context, launcher engine.ProcessLauncher, host, port string, env map[string]string) (engine.Process, bool, error) {
	if attacher, ok := launcher.(engine.ProcessAttacher); ok {
		process, err := attacher.Attach(ctx)
		if err == nil {
			if process == nil {
				return nil, false, fmt.Errorf("opencode engine: attached process is unavailable")
			}
			return process, true, nil
		}
		if !errors.Is(err, engine.ErrNotAttachable) {
			return nil, false, fmt.Errorf("opencode engine: attach existing server: %w", err)
		}
	}
	process, err := launcher.Start(ctx, engine.ProcessRequest{
		Command:               []string{"opencode", "serve", "--hostname", host, "--port", port},
		CWD:                   "/workspace",
		Env:                   env,
		ProviderCredentialEnv: providerCredentialEnv,
		Kind:                  "tool",
		Name:                  "opencode-server",
	})
	if err != nil {
		return nil, false, fmt.Errorf("opencode engine: start server: %w", err)
	}
	return process, false, nil
}

func ensureNativeSession(ctx context.Context, native *client.Client, safe executioncontext.SafeContext, settings settings, recovered bool) (client.Session, bool, error) {
	if recovered {
		listed, err := native.ListSessions(ctx)
		if err != nil {
			return client.Session{}, false, fmt.Errorf("opencode engine: list native sessions: %w", err)
		}
		session, err := pickNativeSession(ctx, native, listed)
		if err != nil {
			return client.Session{}, false, err
		}
		if strings.TrimSpace(session.ID) != "" {
			return session, false, nil
		}
	}
	session, err := native.CreateSession(ctx, client.CreateSessionRequest{
		Model: client.ModelRef{
			ID:         safe.Model.Model,
			ProviderID: settings.ProviderID,
			Variant:    settings.Variant,
		},
	})
	if err != nil {
		return client.Session{}, false, fmt.Errorf("opencode engine: create native session: %w", err)
	}
	return session, true, nil
}

func pickNativeSession(ctx context.Context, native *client.Client, sessions []client.Session) (client.Session, error) {
	if len(sessions) == 0 {
		return client.Session{}, nil
	}
	for _, session := range sessions {
		if strings.TrimSpace(session.ID) == "" {
			continue
		}
		active, err := native.SessionActive(ctx, session.ID)
		if err != nil {
			return client.Session{}, fmt.Errorf("opencode engine: query native session %s: %w", session.ID, err)
		}
		if active {
			return session, nil
		}
	}
	for _, session := range sessions {
		if strings.TrimSpace(session.ID) != "" {
			return session, nil
		}
	}
	return client.Session{}, nil
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
	sections = append(sections, "Work directly in the current project directory and implement the requested issue. If human input is required, use OpenCode's native Question capability rather than guessing.")
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

func nativeStatePollContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, nativeStatePollInterval)
}

func isTransientNativePollTimeout(parent context.Context, err error) bool {
	return err != nil && parent.Err() == nil && errors.Is(err, context.DeadlineExceeded)
}

func queryNativeSessionActive(ctx context.Context, native *client.Client, sessionID string) (bool, error) {
	pollCtx, cancel := nativeStatePollContext(ctx)
	defer cancel()
	return native.SessionActive(pollCtx, sessionID)
}

func reconcilePendingQuestionsForPoll(ctx context.Context, native *client.Client, sessionID string, state *runState) (bool, error) {
	pollCtx, cancel := nativeStatePollContext(ctx)
	defer cancel()
	hadPending, err := reconcilePendingQuestions(pollCtx, native, sessionID, state)
	if isTransientNativePollTimeout(ctx, err) {
		return false, nil
	}
	return hadPending, err
}

func reconcilePendingQuestions(ctx context.Context, native *client.Client, sessionID string, state *runState) (bool, error) {
	pending, err := native.ListQuestions(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("opencode engine: reconcile pending Questions: %w", err)
	}
	hadPending := len(pending) != 0
	if hadPending {
		if err := state.handlePending(ctx, native, pending); err != nil {
			return true, err
		}
	}
	hadAccepted, err := state.reconcileAcceptedReplies(ctx, pending)
	if err != nil {
		return hadPending, err
	}
	return hadPending || hadAccepted, nil
}

func isSessionIdleEvent(event client.Event, sessionID string) (bool, error) {
	if event.Type != "session.idle" {
		return false, nil
	}
	var payload struct {
		SessionID string `json:"sessionID"`
	}
	if err := json.Unmarshal(event.Properties, &payload); err != nil {
		return false, fmt.Errorf("opencode engine: decode session idle event: %w", err)
	}
	return payload.SessionID == sessionID, nil
}

func nativeSessionStallDetail(ctx context.Context, native *client.Client, sessionID string) string {
	active, activeErr := native.SessionActive(ctx, sessionID)
	sessions, listErr := native.ListSessions(ctx)
	return fmt.Sprintf(" (session=%s active=%v activeErr=%v sessions=%d listErr=%v)", sessionID, active, activeErr, len(sessions), listErr)
}

func indicatesSessionExecution(event client.Event, sessionID string) bool {
	if strings.TrimSpace(sessionID) == "" || !eventBelongsToSession(event, sessionID) {
		return false
	}
	eventType := event.Type
	return strings.HasPrefix(eventType, "message.") || strings.HasPrefix(eventType, "session.next.") || strings.HasPrefix(eventType, "question.")
}

func eventBelongsToSession(event client.Event, sessionID string) bool {
	var payload struct {
		SessionID string `json:"sessionID"`
	}
	if json.Unmarshal(event.Properties, &payload) != nil {
		return false
	}
	return payload.SessionID == sessionID
}

type eventReadResult struct {
	event client.Event
	err   error
}

func readEvents(ctx context.Context, stream *client.EventStream) <-chan eventReadResult {
	reads := make(chan eventReadResult, 64)
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

func waitDrained(done <-chan struct{}, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func stopService(parent context.Context, process engine.Process) error {
	return stopServiceWithin(parent, process, serviceTerminateGrace, serviceStopTimeout)
}

func stopServiceWithin(parent context.Context, process engine.Process, terminateGrace, forceTimeout time.Duration) error {
	base := context.WithoutCancel(parent)
	waitCtx, cancelWait := context.WithCancel(base)
	defer cancelWait()
	waitDone := make(chan error, 1)
	go func() {
		_, err := process.Wait(waitCtx)
		waitDone <- err
	}()

	terminateCtx, cancelTerminate := context.WithTimeout(base, forceTimeout)
	terminateErr := process.Terminate(terminateCtx)
	cancelTerminate()

	if terminateErr == nil {
		timer := time.NewTimer(terminateGrace)
		select {
		case waitErr := <-waitDone:
			if !timer.Stop() {
				<-timer.C
			}
			if waitErr != nil {
				return fmt.Errorf("opencode engine: stop native server: %w", waitErr)
			}
			return nil
		case <-timer.C:
		}
	} else {
		select {
		case waitErr := <-waitDone:
			if waitErr != nil {
				return fmt.Errorf("opencode engine: stop native server: %w", errors.Join(terminateErr, waitErr))
			}
			return nil
		default:
		}
	}

	killCtx, cancelKill := context.WithTimeout(base, forceTimeout)
	killErr := process.Kill(killCtx)
	cancelKill()

	timer := time.NewTimer(forceTimeout)
	defer timer.Stop()
	select {
	case waitErr := <-waitDone:
		if waitErr != nil {
			return fmt.Errorf("opencode engine: stop native server: %w", errors.Join(terminateErr, killErr, waitErr))
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("opencode engine: stop native server: %w", errors.Join(terminateErr, killErr, context.DeadlineExceeded))
	}
}

var _ engine.Engine = (*Engine)(nil)
