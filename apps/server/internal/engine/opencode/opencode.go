package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
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
	return &Engine{}
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
	address := e.nativeServerAddress(request.Context)
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return engine.Result{}, fmt.Errorf("opencode engine: parse native server address: %w", err)
	}
	discussionAddress := ""
	if request.IssueDiscussions != nil {
		discussionAddress, err = issueDiscussionBridgeAddress(address, request.Context.Run.ID)
		if err != nil {
			return engine.Result{}, err
		}
		_, discussionPort, splitErr := net.SplitHostPort(discussionAddress)
		if splitErr != nil {
			return engine.Result{}, fmt.Errorf("opencode engine: parse Issue discussion bridge address: %w", splitErr)
		}
		env[issueDiscussionBridgePortEnv] = discussionPort
	}
	process, recovered, err := launchOpenCodeProcessWithDiscussionCapabilities(ctx, request.Launcher, host, port, env, request.IssueStatus != nil, request.IssueComments != nil, request.IssueDiscussions != nil, request.Delegation != nil)
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
	preserveServiceForReconciliation := false
	defer func() {
		if !stopped && !preserveServiceForReconciliation {
			stopped = true
			_ = stopService(ctx, process)
		}
		waitDrained(drainDone, serviceStopTimeout)
	}()

	connector, ok := process.(engine.SessionConnector)
	if !ok {
		return engine.Result{}, fmt.Errorf("opencode engine: process does not support session-local connections")
	}
	native, err := client.NewSession(connector, address)
	if err != nil {
		return engine.Result{}, err
	}
	defer native.CloseIdleConnections()
	native.SetDirectory(nativeWorkingDirectory(process))
	if err := waitHealthy(ctx, native); err != nil {
		return engine.Result{}, err
	}

	var discussionBridgeErrors <-chan error
	if request.IssueDiscussions != nil {
		ids, toolErr := native.ToolIDs(ctx)
		if toolErr != nil {
			return engine.Result{}, fmt.Errorf("opencode engine: load Issue discussion tool: %w", toolErr)
		}
		if !containsOpenCodeTool(ids, issueDiscussionToolName) {
			return engine.Result{}, fmt.Errorf("opencode engine: Issue discussion tool is unavailable")
		}
		bridge, bridgeErr := newIssueDiscussionBridge(connector, discussionAddress, request.IssueDiscussions)
		if bridgeErr != nil {
			return engine.Result{}, bridgeErr
		}
		defer bridge.CloseIdleConnections()
		if bridgeErr := waitIssueDiscussionBridgeHealthy(ctx, bridge); bridgeErr != nil {
			return engine.Result{}, bridgeErr
		}
		bridgeCtx, cancelBridge := context.WithCancel(ctx)
		defer cancelBridge()
		errors := make(chan error, 1)
		discussionBridgeErrors = errors
		go func() {
			errors <- bridge.Serve(bridgeCtx)
		}()
	}

	initialPrompt := initialTaskPromptForRequest(request)
	admissionPrompt := initialPrompt
	if admissions, ok := request.Launcher.(engine.ExecutionAdmissionPromptStore); ok {
		admissionPrompt, err = admissions.GetOrCreateAdmissionPrompt(ctx, process.ID(), initialPrompt)
		if err != nil {
			return engine.Result{}, fmt.Errorf("opencode engine: persist execution admission prompt: %w", err)
		}
	}
	session, promptRequired, err := ensureNativeSession(ctx, native, request.Context, settings, admissionPrompt, recovered)
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
	statusTools := newIssueStatusToolTracker()
	commentTools := newIssueCommentToolTracker()
	delegationTools := newDelegationToolTracker()
	completeDelegationHandoff := func(err error) error {
		if !errors.Is(err, engine.ErrDelegationHandoff) {
			return err
		}
		if boundaryErr := recordExecutionBoundary(ctx, state.activity, "delegation_handoff"); boundaryErr != nil {
			preserveServiceForReconciliation = true
			return fmt.Errorf("opencode engine: persist delegation handoff execution boundary: %w", boundaryErr)
		}
		_ = stream.Close()
		stopped = true
		if stopErr := stopServiceForDelegationHandoff(ctx, process); stopErr != nil {
			waitDrained(drainDone, serviceStopTimeout)
			return stopErr
		}
		waitDrained(drainDone, serviceStopTimeout)
		return err
	}
	if recovered && !promptRequired {
		if err := statusTools.ReconcileAttach(ctx, native, session.ID, request.IssueStatus); err != nil {
			return engine.Result{}, err
		}
		if err := commentTools.Reconcile(ctx, native, session.ID, request.IssueComments); err != nil {
			return engine.Result{}, err
		}
		if err := delegationTools.Reconcile(ctx, native, session.ID, request.Delegation); err != nil {
			return engine.Result{}, completeDelegationHandoff(err)
		}
	}
	state.seedModelUsage(settings.ProviderID, request.Context.Model.Model, nil)
	if limit, err := native.ModelContextLimit(ctx, settings.ProviderID, request.Context.Model.Model); err == nil {
		state.contextLimitTokens = cloneInt64(limit)
	}
	if err := state.backfillUsageFromHistory(ctx, native); err != nil {
		return engine.Result{}, err
	}
	if promptRequired {
		if err := native.Prompt(ctx, session.ID, admissionPrompt); err != nil {
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
		if err := statusTools.Reconcile(ctx, native, session.ID, request.IssueStatus); err != nil {
			return engine.Result{}, err
		}
		if err := commentTools.Reconcile(ctx, native, session.ID, request.IssueComments); err != nil {
			return engine.Result{}, err
		}
		if err := delegationTools.Reconcile(ctx, native, session.ID, request.Delegation); err != nil {
			return engine.Result{}, completeDelegationHandoff(err)
		}
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
		if boundaryErr := recordExecutionBoundary(ctx, state.activity, "completed"); boundaryErr != nil {
			preserveServiceForReconciliation = true
			return engine.Result{}, fmt.Errorf("opencode engine: persist completed execution boundary: %w", boundaryErr)
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
		case bridgeErr := <-discussionBridgeErrors:
			if bridgeErr != nil {
				return engine.Result{}, bridgeErr
			}
			discussionBridgeErrors = nil
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
				if reconcileErr := statusTools.Reconcile(ctx, native, session.ID, request.IssueStatus); reconcileErr != nil {
					return engine.Result{}, errors.Join(
						fmt.Errorf("opencode engine: native event stream disconnected: %w", eventRead.err),
						reconcileErr,
					)
				}
				if reconcileErr := commentTools.Reconcile(ctx, native, session.ID, request.IssueComments); reconcileErr != nil {
					return engine.Result{}, errors.Join(
						fmt.Errorf("opencode engine: native event stream disconnected: %w", eventRead.err),
						reconcileErr,
					)
				}
				if reconcileErr := delegationTools.Reconcile(ctx, native, session.ID, request.Delegation); reconcileErr != nil {
					if errors.Is(reconcileErr, engine.ErrDelegationHandoff) {
						return engine.Result{}, completeDelegationHandoff(reconcileErr)
					}
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
			if err := statusTools.Handle(ctx, eventRead.event, session.ID, request.IssueStatus); err != nil {
				return engine.Result{}, err
			}
			if err := commentTools.Handle(ctx, eventRead.event, session.ID, request.IssueComments); err != nil {
				return engine.Result{}, err
			}
			if err := delegationTools.Handle(ctx, eventRead.event, native, session.ID, request.Delegation); err != nil {
				// A terminal native delegate_task that matches a canonically accepted
				// request is durable handoff evidence. Persist its ordinary tool
				// terminal activity before returning the handoff signal so recovery can
				// correlate native history with the canonical delegation.
				if errors.Is(err, engine.ErrDelegationHandoff) {
					if activityErr := state.handleEvent(ctx, native, eventRead.event); activityErr != nil {
						return engine.Result{}, activityErr
					}
				}
				return engine.Result{}, completeDelegationHandoff(err)
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


func recordExecutionBoundary(ctx context.Context, activity engine.ActivitySink, boundary string) error {
	if activity == nil {
		return nil
	}
	return activity.RecordActivity(ctx, engine.ActivityEvent{
		Type:    "engine.execution.completed",
		Payload: map[string]any{"boundary": boundary},
	})
}

func stopServiceForDelegationHandoff(ctx context.Context, process engine.Process) error {
	if err := stopService(ctx, process); err != nil {
		return fmt.Errorf("opencode engine: delegation handoff service shutdown is uncertain: %w", err)
	}
	return nil
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
		"permission": map[string]any{
			"*":                       "allow",
			delegationPermissionName: "ask",
		},
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
	env := openCodeProcessIsolationEnvironment(safe)
	env["OPENCODE_CONFIG_CONTENT"] = string(encoded)
	return env, nil
}

func launchOpenCodeProcess(ctx context.Context, launcher engine.ProcessLauncher, host, port string, env map[string]string) (engine.Process, bool, error) {
	return launchOpenCodeProcessWithCapabilities(ctx, launcher, host, port, env, true, false, false)
}

func launchOpenCodeProcessWithCapabilities(ctx context.Context, launcher engine.ProcessLauncher, host, port string, env map[string]string, issueStatusEnabled, issueCommentEnabled, delegationEnabled bool) (engine.Process, bool, error) {
	return launchOpenCodeProcessWithDiscussionCapabilities(ctx, launcher, host, port, env, issueStatusEnabled, issueCommentEnabled, false, delegationEnabled)
}

func launchOpenCodeProcessWithDiscussionCapabilities(ctx context.Context, launcher engine.ProcessLauncher, host, port string, env map[string]string, issueStatusEnabled, issueCommentEnabled, issueDiscussionEnabled, delegationEnabled bool) (engine.Process, bool, error) {
	if attacher, ok := launcher.(engine.ProcessAttacher); ok {
		process, err := attacher.Attach(ctx)
		if err == nil {
			if process == nil {
				return nil, false, fmt.Errorf("opencode engine: attached process is unavailable")
			}
			return reconcileAttachedOpenCodeCapabilitiesWithDiscussion(ctx, launcher, process, host, port, env, issueStatusEnabled, issueCommentEnabled, issueDiscussionEnabled, delegationEnabled)
		}
		if !errors.Is(err, engine.ErrNotAttachable) {
			return nil, false, fmt.Errorf("opencode engine: attach existing server: %w", err)
		}
	}
	process, err := startOpenCodeProcessWithDiscussion(ctx, launcher, host, port, env, issueStatusEnabled, issueCommentEnabled, issueDiscussionEnabled, delegationEnabled)
	if err != nil {
		return nil, false, err
	}
	return process, false, nil
}

func containsOpenCodeTool(ids []string, name string) bool {
	for _, id := range ids {
		if id == name {
			return true
		}
	}
	return false
}

func ensureNativeSession(ctx context.Context, native *client.Client, safe executioncontext.SafeContext, settings settings, expectedPrompt string, recovered bool) (client.Session, bool, error) {
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
			admitted, err := native.PromptAdmitted(ctx, session.ID, expectedPrompt)
			if err != nil {
				return client.Session{}, false, fmt.Errorf("opencode engine: prove recovered prompt admission: %w", err)
			}
			return session, !admitted, nil
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
	bound := path.Clean(strings.TrimSpace(native.Directory()))
	candidates := make([]client.Session, 0, len(sessions))
	for _, session := range sessions {
		if strings.TrimSpace(session.ID) == "" {
			continue
		}
		if bound != "." && bound != "" {
			directory := path.Clean(strings.TrimSpace(session.Directory))
			if directory != bound {
				continue
			}
		}
		candidates = append(candidates, session)
	}
	for _, session := range candidates {
		active, err := native.SessionActive(ctx, session.ID)
		if err != nil {
			return client.Session{}, fmt.Errorf("opencode engine: query native session %s: %w", session.ID, err)
		}
		if active {
			return session, nil
		}
	}
	if len(candidates) != 0 {
		return candidates[0], nil
	}
	return client.Session{}, nil
}

func nativeWorkingDirectory(process engine.Process) string {
	if provider, ok := process.(engine.WorkingDirectoryProvider); ok {
		if directory := strings.TrimSpace(provider.WorkingDirectory()); directory != "" {
			return directory
		}
	}
	return runtimepkg.WorkspaceTarget
}

const issueStatusPromptGuidance = "Issue Board status is an explicit workflow decision. Use set_issue_status(status) for the current Issue when the Board state should change. When meaningful work starts, use IN_PROGRESS. When you cannot continue, use BLOCKED. When implementation or other work is complete and ready for human review or handoff, use REVIEW. Completing the requested implementation does not by itself mean DONE. For normal coding or implementation work, a successful final handoff should therefore normally leave the Issue in REVIEW, not DONE. Use DONE only when the Issue is fully finished and no human review, approval, or handoff remains. Before your final response, compare the final work outcome with the persisted Issue Board status and call set_issue_status(status) if the Board state should now be different. Do not infer Board status from the Run lifecycle, and do not use status changes as a substitute for OpenCode's native Question capability when human input is required."

const issueCommentPromptGuidance = "Issue comments are deliberate durable collaboration. Use publish_issue_comment(body) only for concise findings/results, handoffs/conclusions, non-blocking collaboration-level questions, or pointers to existing Run/Review evidence. Do not copy raw command output, test output, file contents, logs, ordinary progress, or hidden reasoning into Issue comments. Use OpenCode's native Question capability for blocking human input. Plain @name text in a comment has no routing or execution semantics. Publishing a comment does not change Issue ownership or Board status."

const delegationPromptGuidance = "This Run may request bounded help from another Agent with delegate_task(targetAgentId, task). Choose targetAgentId only from the server-provided available delegation targets; never guess Agent identifiers. Delegation does not transfer Issue ownership or Review authority."

func initialTaskPromptForRequest(request engine.Request) string {
	var delegationContext *engine.DelegationToolContext
	if request.Delegation != nil {
		delegationContext = request.DelegationContext
	}
	prompt := initialTaskPromptWithDelegationToolContext(request.Context, delegationContext, request.DelegationContinuation)
	if request.IssueComments != nil {
		prompt += "\n\n" + issueCommentPromptGuidance
	}
	return prompt
}

func initialTaskPrompt(safe executioncontext.SafeContext) string {
	return initialTaskPromptWithDelegationContinuation(safe, nil)
}

func initialTaskPromptWithDelegationContinuation(safe executioncontext.SafeContext, continuation *engine.DelegationContinuation) string {
	var delegationContext *engine.DelegationToolContext
	if safe.Delegation == nil && safe.Agent.AllowDelegation {
		delegationContext = &engine.DelegationToolContext{}
	}
	return initialTaskPromptWithDelegationToolContext(safe, delegationContext, continuation)
}

func initialTaskPromptWithDelegationToolContext(safe executioncontext.SafeContext, delegationContext *engine.DelegationToolContext, continuation *engine.DelegationContinuation) string {
	var sections []string
	if role := strings.TrimSpace(safe.Agent.RoleInstructions); role != "" {
		sections = append(sections, "Agent role instructions:\n"+role)
	}
	issue := "Issue: " + strings.TrimSpace(safe.Issue.Title)
	if description := strings.TrimSpace(safe.Issue.Description); description != "" {
		issue += "\n\n" + description
	}
	sections = append(sections, issue)
	if safe.Delegation != nil && strings.TrimSpace(safe.Delegation.Task) != "" {
		sections = append(sections, "Delegated task:\n"+strings.TrimSpace(safe.Delegation.Task))
	}
	if safe.ReviewFeedback != nil && strings.TrimSpace(safe.ReviewFeedback.Feedback) != "" {
		sections = append(sections, "Review feedback:\n"+strings.TrimSpace(safe.ReviewFeedback.Feedback))
	}
	if continuation != nil {
		resultEventID := strings.TrimSpace(continuation.ResultEventID)
		if resultEventID == "" {
			resultEventID = "none"
		}
		sections = append(sections, fmt.Sprintf(
			"Delegation result returned to this parent Run:\nDelegation ID: %s\nTarget Agent ID: %s\nDelegated task: %s\nOutcome: %s\nResult summary: %s\nDelegated Run ID: %s\nResult evidence Event ID: %s\nWorkspace changes accepted: %t\nCurrent Issue Workspace revision: %s\nContinue as the authoritative parent using the current Workspace state. Do not replay the delegated task merely to reconstruct its result.",
			strings.TrimSpace(continuation.DelegationID),
			strings.TrimSpace(continuation.TargetAgentID),
			strings.TrimSpace(continuation.Task),
			strings.TrimSpace(continuation.Outcome),
			strings.TrimSpace(continuation.ResultSummary),
			strings.TrimSpace(continuation.DelegatedRunID),
			resultEventID,
			continuation.WorkspaceChangesAccepted,
			strings.TrimSpace(continuation.WorkspaceRevision),
		))
	}
	if safe.Delegation == nil {
		sections = append(sections, "Current persisted Issue Board status: "+strings.TrimSpace(safe.Issue.Status)+".\n"+issueStatusPromptGuidance)
		if delegationContext != nil {
			sections = append(sections, delegationPromptGuidance+"\n\n"+delegationToolContextPrompt(delegationContext))
		}
	} else {
		sections = append(sections, "This is delegated execution. Work only on the bounded delegated task. The parent Run remains authoritative for Issue ownership and Board/Review outcome; do not attempt to change Issue status or delegate further work.")
	}
	sections = append(sections, "Work directly in the current project directory and implement the requested issue. Treat /workspace as the logical workspace root: use project-relative paths for workspace files rather than absolute /workspace paths. If human input is required, use OpenCode's native Question capability rather than guessing.")
	return strings.Join(sections, "\n\n")
}

func delegationToolContextPrompt(context *engine.DelegationToolContext) string {
	if context == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("Available delegation targets:")
	if len(context.Targets) == 0 {
		builder.WriteString("\n- none")
	} else {
		for _, target := range context.Targets {
			builder.WriteString("\n- ")
			builder.WriteString(strings.TrimSpace(target.ID))
			builder.WriteString(" — ")
			builder.WriteString(strings.TrimSpace(target.Name))
		}
	}
	if context.Squad == nil {
		return builder.String()
	}
	builder.WriteString("\n\nCurrent Issue Squad: ")
	builder.WriteString(strings.TrimSpace(context.Squad.Name))
	builder.WriteString(" (")
	builder.WriteString(strings.TrimSpace(context.Squad.ID))
	builder.WriteString(")")
	builder.WriteString("\nAuthoritative Squad leader: ")
	builder.WriteString(strings.TrimSpace(context.Squad.LeaderAgentName))
	builder.WriteString(" (")
	builder.WriteString(strings.TrimSpace(context.Squad.LeaderAgentID))
	builder.WriteString(")")
	builder.WriteString("\nSquad Agent members:")
	if len(context.Squad.Members) == 0 {
		builder.WriteString("\n- none")
	} else {
		for _, member := range context.Squad.Members {
			builder.WriteString("\n- ")
			builder.WriteString(strings.TrimSpace(member.ID))
			builder.WriteString(" — ")
			builder.WriteString(strings.TrimSpace(member.Name))
			if member.Role != nil && strings.TrimSpace(*member.Role) != "" {
				builder.WriteString(" — role: ")
				builder.WriteString(strings.TrimSpace(*member.Role))
			}
		}
	}
	builder.WriteString("\nSquad membership is collaboration context only. It does not grant delegation permission or make an Agent a valid target unless that Agent is also listed under Available delegation targets.")
	return builder.String()
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
