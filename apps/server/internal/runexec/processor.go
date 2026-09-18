package runexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

const (
	cleanupTimeout                    = 30 * time.Second
	processCancellationCleanupTimeout = 5 * time.Second
)

type ContextResolver interface {
	Resolve(context.Context, string, string) (executioncontext.Resolved, error)
}

type ExecutionStore interface {
	executioncontext.ProvenanceStore
	ListExecutionSessions(context.Context, string, []string) ([]store.ExecutionSession, error)
	GetWorkspace(context.Context, string, string) (store.Workspace, error)
	UpdateWorkspaceCurrentBranch(context.Context, string, string, string) (store.Workspace, error)
}

type delegationRecoveryEventStore interface {
	ListRunEvents(context.Context, string, string, int64, int) ([]store.Event, error)
}

type delegationHandoffRecoveryStore interface {
	store.DelegationStore
	store.DelegationWorkspaceHandoffStore
	delegationRecoveryEventStore
}

type delegatedTerminalRecoveryStore interface {
	store.DelegationStore
	delegationRecoveryEventStore
}

type delegatedParentRunStore interface {
	store.DelegationStore
	GetRun(context.Context, string, string) (store.Run, error)
}

type executionSessionCanceller interface {
	Cancel(context.Context, string, string, time.Duration) error
}

type SessionService interface {
	Start(context.Context, string, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error)
	Attach(context.Context, string, string) (*app.AuthorizedExecutionProcess, error)
	ReconcileAll(context.Context) error
}

type runRedactionReleaser interface {
	ReleaseRunRedaction(string)
}

type RuntimeService interface {
	Create(context.Context, string, string, string) (store.RuntimeInstance, error)
	Start(context.Context, string, string) (store.RuntimeInstance, error)
	Stop(context.Context, string, string, runtimepkg.StopReason) (store.RuntimeInstance, error)
	Destroy(context.Context, string, string) (store.RuntimeInstance, error)
}

type RunnerConnector interface {
	Connect(context.Context, string, string) (runnerClient, error)
}

type IssueWorkspaceEnsurer interface {
	EnsureIssueWorkspace(context.Context, string, string) (store.Workspace, error)
}

type runnerClient interface {
	SendTransfer(context.Context, string, string, string, []byte, runner.TransferProgressFunc) error
	ReceiveTransfer(context.Context, string, runner.TransferProgressFunc) (string, []byte, error)
	ConfirmTransferApplied(context.Context, string, string) error
}

type runnerSessionPreparer interface {
	CreateRunnerSession(context.Context, string, string, string) (store.ExecutionSession, error)
}

type Processor struct {
	store      ExecutionStore
	resolver   ContextResolver
	runtimes   RuntimeService
	runners    RunnerConnector
	sessions   SessionService
	engines    *engine.Registry
	events     *evidence.Recorder
	output     *evidence.OutputRecorder
	branches   *branchObserver
	git        workspace.Git
	workspaces IssueWorkspaceEnsurer
}

func NewProcessor(
	store ExecutionStore,
	resolver ContextResolver,
	runtimes RuntimeService,
	sessions SessionService,
	engines *engine.Registry,
	events *evidence.Recorder,
	output *evidence.OutputRecorder,
	git workspace.Git,
	runners RunnerConnector,
) (*Processor, error) {
	if store == nil || resolver == nil || sessions == nil || engines == nil || events == nil || output == nil {
		return nil, fmt.Errorf("run execution: all processor dependencies are required")
	}
	return &Processor{
		store: store, resolver: resolver, runtimes: runtimes, runners: runners, sessions: sessions, engines: engines,
		events: events, output: output, branches: newBranchObserver(store, git, events), git: git,
	}, nil
}

func (p *Processor) SetWorkspaceEnsurer(ensurer IssueWorkspaceEnsurer) {
	if p != nil {
		p.workspaces = ensurer
	}
}

func (p *Processor) Process(ctx context.Context, claim *store.SchedulerAdmission, lifecycle scheduler.Lifecycle) (scheduler.Result, error) {
	if claim == nil || lifecycle == nil {
		return scheduler.Result{}, fmt.Errorf("run execution: scheduler admission and lifecycle are required")
	}
	run, err := lifecycle.Running(ctx)
	if err != nil {
		return scheduler.Result{}, err
	}
	if redactions, ok := p.sessions.(runRedactionReleaser); ok {
		defer redactions.ReleaseRunRedaction(run.ID)
	}
	resolved, err := p.resolver.Resolve(ctx, run.ProjectID, run.ID)
	if err != nil {
		return failed(err), nil
	}
	delegationContinuation, err := p.loadDelegationContinuation(ctx, claim, run)
	if err != nil {
		return failed(err), nil
	}
	live, err := p.liveExecutionSession(ctx, run)
	if err != nil {
		return failed(err), nil
	}
	if live != nil {
		return p.attachExistingExecution(ctx, run, resolved.Safe, *live, delegationContinuation)
	}
	return p.startNewExecution(ctx, claim, run, resolved.Safe, delegationContinuation)
}

func (p *Processor) startNewExecution(ctx context.Context, claim *store.SchedulerAdmission, run store.Run, safe executioncontext.SafeContext, delegationContinuation *engine.DelegationContinuation) (scheduler.Result, error) {
	runEventType := "run.started"
	if claim.Job.Kind == "RESUME" {
		runEventType = "run.resumed"
	}
	if err := p.record(ctx, safe, runEventType, nil, nil, nil); err != nil {
		return scheduler.Result{}, err
	}
	p.branches.observeIfChanged(ctx, safe, nil)

	runnerID := strings.TrimSpace(claim.RunnerID)
	if runnerID != "" {
		if !isRemoteGitProject(safe) {
			ready, err := p.ensureRunnerWorkspace(ctx, run, safe)
			if err != nil {
				return failed(err), nil
			}
			safe = ready
		}
		preparer, ok := p.sessions.(runnerSessionPreparer)
		if !ok {
			return failed(fmt.Errorf("runner session preparer is unavailable")), nil
		}
		session, err := preparer.CreateRunnerSession(ctx, run.ProjectID, run.ID, runnerID)
		if err != nil {
			return failed(err), nil
		}
		safe, err = p.prepareRunnerSessionWorkspace(ctx, run, safe, runnerID, session.ID)
		if err != nil {
			return failed(err), nil
		}
		return p.runEngineOnRunnerWithContinuation(ctx, run, safe, runnerID, session.ID, delegationContinuation)
	}
	if p.runtimes == nil {
		return failed(fmt.Errorf("scheduler admission is missing runner id")), nil
	}
	if err := p.record(ctx, safe, "runtime.provisioning", map[string]any{"runtimeId": safe.Runtime.ID}, nil, nil); err != nil {
		return scheduler.Result{}, err
	}
	instance, err := p.acquireRuntime(ctx, run.ProjectID, run.IssueID, safe.Runtime.ID)
	if err != nil {
		return failed(err), nil
	}
	started, err := p.runtimes.Start(ctx, run.ProjectID, instance.ID)
	if err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(err, cleanupErr)), nil
	}
	instance, safe, err = p.materializeExecution(ctx, run, safe, started)
	if err != nil {
		return failed(err), nil
	}
	if err := p.record(ctx, safe, "runtime.started", map[string]any{"runtimeId": safe.Runtime.ID}, &instance.ID, nil); err != nil {
		_ = p.cleanupRuntime(ctx, safe, instance)
		return scheduler.Result{}, err
	}
	return p.runEngineWithContinuation(ctx, run, safe, instance, "", delegationContinuation)
}

func (p *Processor) attachExistingExecution(ctx context.Context, run store.Run, safe executioncontext.SafeContext, session store.ExecutionSession, delegationContinuation *engine.DelegationContinuation) (scheduler.Result, error) {
	if err := p.record(ctx, safe, "run.resumed", map[string]any{"reason": "lease_reconciliation"}, nil, nil); err != nil {
		return scheduler.Result{}, err
	}
	p.branches.observeIfChanged(ctx, safe, nil)
	if session.RunnerID != "" {
		if session.Status == "PENDING" {
			if !isRemoteGitProject(safe) {
				ready, err := p.ensureRunnerWorkspace(ctx, run, safe)
				if err != nil {
					return failed(err), nil
				}
				safe = ready
			}
			var err error
			safe, err = p.prepareRunnerSessionWorkspace(ctx, run, safe, session.RunnerID, session.ID)
			if err != nil {
				return failed(err), nil
			}
		} else {
			safe = p.attachRunnerProvenance(ctx, safe, session.RunnerID)
		}
		return p.runEngineOnRunnerWithContinuation(ctx, run, safe, session.RunnerID, session.ID, delegationContinuation)
	}
	if p.runtimes == nil {
		return failed(fmt.Errorf("legacy runtime execution is unavailable")), nil
	}
	started, err := p.runtimes.Start(ctx, run.ProjectID, session.RuntimeInstanceID)
	if err != nil {
		return failed(err), nil
	}
	instance, safe, err := p.materializeExecution(ctx, run, safe, started)
	if err != nil {
		return failed(err), nil
	}
	return p.runEngineWithContinuation(ctx, run, safe, instance, session.ID, delegationContinuation)
}

func (p *Processor) materializeExecution(ctx context.Context, run store.Run, safe executioncontext.SafeContext, instance store.RuntimeInstance) (store.RuntimeInstance, executioncontext.SafeContext, error) {
	// Runtime creation is also the boundary that materializes a placeholder
	// Issue Workspace. Resolve again after that boundary so immutable provenance
	// and Engine context refer to the same durable Workspace that is mounted.
	materialized, err := p.resolver.Resolve(ctx, run.ProjectID, run.ID)
	if err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return store.RuntimeInstance{}, executioncontext.SafeContext{}, errors.Join(err, cleanupErr)
	}
	if materialized.Safe.Runtime.ID != safe.Runtime.ID || materialized.Safe.Workspace.ID != safe.Workspace.ID {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return store.RuntimeInstance{}, executioncontext.SafeContext{}, errors.Join(fmt.Errorf("run execution: execution bindings changed during Runtime provisioning"), cleanupErr)
	}
	safe = materialized.Safe
	if err := executioncontext.EnsureProvenance(ctx, p.store, run.ProjectID, run.ID, safe); err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return store.RuntimeInstance{}, executioncontext.SafeContext{}, errors.Join(err, cleanupErr)
	}
	return instance, safe, nil
}

func (p *Processor) runEngine(ctx context.Context, run store.Run, safe executioncontext.SafeContext, instance store.RuntimeInstance, attachSessionID string) (scheduler.Result, error) {
	return p.runEngineWithContinuation(ctx, run, safe, instance, attachSessionID, nil)
}

func (p *Processor) runEngineWithContinuation(ctx context.Context, run store.Run, safe executioncontext.SafeContext, instance store.RuntimeInstance, attachSessionID string, delegationContinuation *engine.DelegationContinuation) (scheduler.Result, error) {
	adapter, err := p.engines.Get(safe.Agent.Engine)
	if err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(err, cleanupErr)), nil
	}
	launcher := &processLauncher{
		sessions:          p.sessions,
		events:            p.events,
		output:            p.output,
		safe:              safe,
		runtimeInstanceID: instance.ID,
		attachSessionID:   attachSessionID,
		scope:             evidence.RunScope{ProjectID: run.ProjectID, IssueID: run.IssueID, RunID: run.ID},
		branches:          p.branches,
	}
	request, err := p.engineRequestWithDelegationContinuation(ctx, safe, launcher, instance.ID, delegationContinuation)
	if err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(err, cleanupErr)), nil
	}
	engineResult, engineErr := adapter.Execute(ctx, request)
	if errors.Is(engineErr, engine.ErrWaitingForInput) {
		return p.finishWaitingForInput(ctx, safe, instance)
	}
	handoff, handoffRequested := engine.AsDelegationHandoff(engineErr)
	if engineErr != nil && !handoffRequested {
		if uncertaintyErr := p.executionUncertainty(ctx, run, engineErr); uncertaintyErr != nil {
			return scheduler.Result{}, uncertaintyErr
		}
	}
	if engineErr == nil || handoffRequested {
		boundary := "completed"
		if handoffRequested {
			boundary = "delegation_handoff"
		}
		if err := p.record(ctx, safe, "engine.execution.completed", map[string]any{"boundary": boundary}, &instance.ID, nil); err != nil {
			cleanupErr := p.cleanupRuntime(ctx, safe, instance)
			return p.failExecution(ctx, safe, errors.Join(err, cleanupErr), &instance.ID)
		}
	}

	finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	_, finalizeErr := p.finalizeServerWorkspace(finalizeCtx, safe)
	if finalizeErr == nil {
		finalizeErr = p.recordDelegationWorkspaceAccepted(finalizeCtx, safe, &instance.ID)
	}
	cancelFinalize()
	if engineErr == nil && finalizeErr == nil && engineResult.Summary != "" {
		engineErr = p.record(ctx, safe, "agent.message", map[string]any{"message": engineResult.Summary}, &instance.ID, nil)
	}

	cleanupErr := p.cleanupRuntime(ctx, safe, instance)
	if ctx.Err() != nil {
		return scheduler.Result{}, ctx.Err()
	}
	if handoffRequested {
		if combined := errors.Join(finalizeErr, cleanupErr); combined != nil {
			return p.failExecution(ctx, safe, combined, &instance.ID)
		}
		return p.finishDelegationWorkspaceHandoff(ctx, safe, handoff, &instance.ID)
	}
	if combined := errors.Join(engineErr, finalizeErr, cleanupErr); combined != nil {
		return p.failExecution(ctx, safe, combined, &instance.ID)
	}
	return p.finishSuccessfulExecution(ctx, safe, &instance.ID)
}

func (p *Processor) executionUncertainty(ctx context.Context, run store.Run, cause error) error {
	if cause == nil {
		return nil
	}
	live, err := p.liveExecutionSession(ctx, run)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("run execution: inspect Execution Session after execution error: %w", err))
	}
	if live == nil {
		return nil
	}
	return errors.Join(cause, fmt.Errorf(
		"run execution: Execution Session %s remains %s after execution error; reconciliation is required",
		live.ID, live.Status,
	))
}

func (p *Processor) liveExecutionSession(ctx context.Context, run store.Run) (*store.ExecutionSession, error) {
	sessions, err := p.store.ListExecutionSessions(ctx, run.ProjectID, []string{"PENDING", "STARTING", "RUNNING"})
	if err != nil {
		return nil, err
	}
	var live []store.ExecutionSession
	for _, session := range sessions {
		if session.RunID != run.ID {
			continue
		}
		switch session.Status {
		case "PENDING", "STARTING", "RUNNING":
			live = append(live, session)
		}
	}
	if len(live) > 1 {
		return nil, fmt.Errorf("run execution: multiple live Execution Sessions exist for Run %s", run.ID)
	}
	if len(live) == 0 {
		return nil, nil
	}
	return &live[0], nil
}

func (p *Processor) Reconcile(ctx context.Context, claim *store.SchedulerAdmission) (store.SchedulerReconciliationOutcome, *string, error) {
	if claim == nil {
		return store.SchedulerReconciliationUnknown, nil, fmt.Errorf("run execution: scheduler admission is required")
	}
	if err := p.sessions.ReconcileAll(ctx); err != nil {
		return store.SchedulerReconciliationUnknown, nil, err
	}
	sessions, err := p.store.ListExecutionSessions(ctx, claim.Run.ProjectID, []string{"PENDING", "STARTING", "RUNNING", "COMPLETED", "FAILED", "CANCELLED"})
	if err != nil {
		return store.SchedulerReconciliationUnknown, nil, err
	}
	parentTerminal, err := p.delegatedParentTerminal(ctx, claim.Run)
	if err != nil {
		return store.SchedulerReconciliationUnknown, nil, err
	}
	if parentTerminal {
		return p.reconcileDelegatedRunWithTerminalParent(ctx, claim.Run, sessions)
	}
	seen := false
	for _, session := range sessions {
		if session.RunID != claim.Run.ID {
			continue
		}
		seen = true
		switch session.Status {
		case "PENDING", "STARTING", "RUNNING":
			return store.SchedulerReconciliationActive, nil, nil
		}
	}
	if !seen {
		return store.SchedulerReconciliationRetry, nil, nil
	}
	// Once any external Execution Session existed, lack of a live session is not
	// enough proof that replaying the Engine is safe. Delegation recovery has two
	// narrow exceptions that never replay the Engine: a delegated child may be
	// finalized from one authoritative terminal session, while a parent may finish
	// a previously recorded Workspace handoff. All ambiguous ownership remains
	// UNKNOWN.
	outcome, reason, recovered, err := p.recoverDelegatedTerminalRun(ctx, claim.Run, sessions, false)
	if err != nil {
		return store.SchedulerReconciliationUnknown, nil, err
	}
	if recovered {
		return outcome, reason, nil
	}
	if err := p.recoverDelegationWorkspaceHandoff(ctx, claim.Run, sessions); err != nil {
		return store.SchedulerReconciliationUnknown, nil, err
	}
	return store.SchedulerReconciliationUnknown, nil, nil
}

func (p *Processor) delegatedParentTerminal(ctx context.Context, child store.Run) (bool, error) {
	lineage, ok := p.store.(delegatedParentRunStore)
	if !ok {
		return false, nil
	}
	delegation, err := lineage.GetDelegationByRun(ctx, child.ProjectID, child.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	parent, err := lineage.GetRun(ctx, child.ProjectID, delegation.ParentRunID)
	if err != nil {
		return false, err
	}
	if delegation.IssueID != child.IssueID || parent.IssueID != child.IssueID || parent.WorkspaceID != child.WorkspaceID || parent.AgentID == nil || *parent.AgentID != delegation.ParentAgentID || child.AgentID == nil || *child.AgentID != delegation.TargetAgentID {
		return false, fmt.Errorf("run execution: delegated parent lineage does not match authoritative Runs")
	}
	switch parent.Status {
	case "COMPLETED", "FAILED", "CANCELLED":
		return true, nil
	default:
		return false, nil
	}
}

func (p *Processor) reconcileDelegatedRunWithTerminalParent(ctx context.Context, run store.Run, sessions []store.ExecutionSession) (store.SchedulerReconciliationOutcome, *string, error) {
	live := liveExecutionSessionsForRun(sessions, run.ID)
	if len(live) > 1 {
		return store.SchedulerReconciliationUnknown, nil, nil
	}
	if len(live) == 1 {
		canceller, ok := p.sessions.(executionSessionCanceller)
		if !ok {
			return store.SchedulerReconciliationUnknown, nil, fmt.Errorf("run execution: execution session cancellation is unavailable")
		}
		if err := canceller.Cancel(ctx, run.ProjectID, live[0].ID, processCancellationCleanupTimeout); err != nil {
			return store.SchedulerReconciliationUnknown, nil, err
		}
		if err := p.sessions.ReconcileAll(ctx); err != nil {
			return store.SchedulerReconciliationUnknown, nil, err
		}
		var err error
		sessions, err = p.store.ListExecutionSessions(ctx, run.ProjectID, []string{"PENDING", "STARTING", "RUNNING", "COMPLETED", "FAILED", "CANCELLED"})
		if err != nil {
			return store.SchedulerReconciliationUnknown, nil, err
		}
	}

	matching := executionSessionsForRun(sessions, run.ID)
	if len(matching) == 0 {
		return store.SchedulerReconciliationCancelled, nil, nil
	}
	if len(liveExecutionSessionsForRun(matching, run.ID)) != 0 {
		return store.SchedulerReconciliationUnknown, nil, nil
	}
	outcome, reason, recovered, err := p.recoverDelegatedTerminalRun(ctx, run, matching, true)
	if err != nil {
		return store.SchedulerReconciliationUnknown, nil, err
	}
	if recovered {
		return outcome, reason, nil
	}
	return store.SchedulerReconciliationUnknown, nil, nil
}

func executionSessionsForRun(sessions []store.ExecutionSession, runID string) []store.ExecutionSession {
	matching := make([]store.ExecutionSession, 0, len(sessions))
	for _, session := range sessions {
		if session.RunID == runID {
			matching = append(matching, session)
		}
	}
	return matching
}

func liveExecutionSessionsForRun(sessions []store.ExecutionSession, runID string) []store.ExecutionSession {
	matching := make([]store.ExecutionSession, 0, 1)
	for _, session := range sessions {
		if session.RunID != runID {
			continue
		}
		switch session.Status {
		case "PENDING", "STARTING", "RUNNING":
			matching = append(matching, session)
		}
	}
	return matching
}

const delegationRecoveryEventPageSize = 500

func (p *Processor) recoverDelegatedTerminalRun(ctx context.Context, run store.Run, sessions []store.ExecutionSession, cancellationAuthoritative bool) (store.SchedulerReconciliationOutcome, *string, bool, error) {
	recovery, ok := p.store.(delegatedTerminalRecoveryStore)
	if !ok {
		return "", nil, false, nil
	}
	delegation, err := recovery.GetDelegationByRun(ctx, run.ProjectID, run.ID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil, false, nil
	}
	if err != nil {
		return store.SchedulerReconciliationUnknown, nil, true, err
	}
	if run.AgentID == nil || *run.AgentID != delegation.TargetAgentID {
		return store.SchedulerReconciliationUnknown, nil, true, fmt.Errorf("run execution: delegated child Agent lineage does not match canonical delegation")
	}
	session, ok := terminalDelegatedExecutionSession(sessions, run.ID)
	if !ok {
		return store.SchedulerReconciliationUnknown, nil, true, nil
	}
	events, err := listDelegationRecoveryEvents(ctx, recovery, run.ProjectID, run.ID)
	if err != nil {
		return store.SchedulerReconciliationUnknown, nil, true, err
	}
	if !delegationWorkspaceAcceptedEvidence(events, delegation.ID) {
		safe, err := p.delegationRecoveryContext(ctx, run)
		if err != nil {
			return store.SchedulerReconciliationUnknown, nil, true, err
		}
		if safe.Delegation == nil || safe.Delegation.ID != delegation.ID || safe.Delegation.ParentRunID != delegation.ParentRunID {
			return store.SchedulerReconciliationUnknown, nil, true, fmt.Errorf("run execution: delegated recovery provenance does not match canonical delegation")
		}
		syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		switch {
		case strings.TrimSpace(session.RunnerID) != "":
			err = p.syncWorkspaceFromRunner(syncCtx, safe, session.RunnerID, session.ID)
		case strings.TrimSpace(session.RuntimeInstanceID) != "":
			_, err = p.finalizeServerWorkspace(syncCtx, safe)
		default:
			err = fmt.Errorf("run execution: terminal delegated session has no recoverable execution owner")
		}
		if err == nil {
			err = p.recordDelegationWorkspaceAccepted(syncCtx, safe, optionalEventID(session.RuntimeInstanceID))
		}
		cancel()
		if err != nil {
			return store.SchedulerReconciliationUnknown, nil, true, err
		}
	}
	switch recoveredExecutionOutcome(session, events, "completed", cancellationAuthoritative) {
	case recoveredExecutionSucceeded:
		return store.SchedulerReconciliationCompleted, nil, true, nil
	case recoveredExecutionFailed:
		reason := delegatedRecoveryFailureReason(events)
		return store.SchedulerReconciliationFailed, &reason, true, nil
	case recoveredExecutionCancelled:
		return store.SchedulerReconciliationCancelled, nil, true, nil
	default:
		return store.SchedulerReconciliationUnknown, nil, true, nil
	}
}

func terminalDelegatedExecutionSession(sessions []store.ExecutionSession, runID string) (store.ExecutionSession, bool) {
	var candidate store.ExecutionSession
	for _, session := range sessions {
		if session.RunID != runID {
			continue
		}
		switch session.Status {
		case "COMPLETED", "FAILED", "CANCELLED":
		default:
			continue
		}
		if strings.TrimSpace(session.RunnerID) == "" && strings.TrimSpace(session.RuntimeInstanceID) == "" {
			return store.ExecutionSession{}, false
		}
		if candidate.ID != "" && candidate.ID != session.ID {
			return store.ExecutionSession{}, false
		}
		candidate = session
	}
	return candidate, candidate.ID != ""
}

type recoveredExecutionState string

const (
	recoveredExecutionUnknown   recoveredExecutionState = "unknown"
	recoveredExecutionSucceeded recoveredExecutionState = "succeeded"
	recoveredExecutionFailed    recoveredExecutionState = "failed"
	recoveredExecutionCancelled recoveredExecutionState = "cancelled"
)

func recoveredExecutionOutcome(session store.ExecutionSession, events []store.Event, successBoundary string, cancellationAuthoritative bool) recoveredExecutionState {
	switch session.Status {
	case "COMPLETED":
		return recoveredExecutionSucceeded
	case "FAILED":
		return recoveredExecutionFailed
	case "CANCELLED":
		if cancellationAuthoritative || hasRunEvent(events, "run.cancellation_requested") {
			return recoveredExecutionCancelled
		}
		for _, event := range events {
			if event.Type != "engine.execution.completed" {
				continue
			}
			var payload struct {
				Boundary string `json:"boundary"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil && payload.Boundary == successBoundary {
				return recoveredExecutionSucceeded
			}
		}
	}
	return recoveredExecutionUnknown
}

func hasRunEvent(events []store.Event, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func delegationWorkspaceAcceptedEvidence(events []store.Event, delegationID string) bool {
	for _, event := range events {
		if event.Type == "delegation.workspace_accepted" {
			var payload struct {
				DelegationID string `json:"delegationId"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil && payload.DelegationID == delegationID {
				return true
			}
		}
		if authoritativeWorkspaceHandbackEvent(event) {
			return true
		}
	}
	return false
}

func authoritativeWorkspaceHandbackEvent(event store.Event) bool {
	if event.Type != "workspace.transfer.completed" {
		return false
	}
	var payload struct {
		Direction string `json:"direction"`
	}
	if json.Unmarshal(event.Payload, &payload) != nil {
		return false
	}
	return payload.Direction == "from_runner" || payload.Direction == "git_publish"
}

func delegatedRecoveryFailureReason(events []store.Event) string {
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type != "run.failed" {
			continue
		}
		var payload struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(events[index].Payload, &payload) == nil && strings.TrimSpace(payload.Reason) != "" {
			return safeFailure(errors.New(payload.Reason))
		}
	}
	return "delegated execution failed during reconciliation"
}

func (p *Processor) recoverDelegationWorkspaceHandoff(ctx context.Context, run store.Run, sessions []store.ExecutionSession) error {
	recovery, ok := p.store.(delegationHandoffRecoveryStore)
	if !ok {
		return nil
	}
	delegations, err := recovery.ListDelegationsByParentRun(ctx, run.ProjectID, run.ID)
	if err != nil {
		return err
	}
	if len(delegations) == 0 {
		return nil
	}
	events, err := listDelegationRecoveryEvents(ctx, recovery, run.ProjectID, run.ID)
	if err != nil {
		return err
	}
	for _, delegation := range delegations {
		completed, synchronized := delegationRecoveryEvidence(events, delegation)
		if !completed {
			continue
		}
		// ExecutionSession status is transport state. In particular OpenCode may
		// intentionally terminate its service process after a successful handoff,
		// which persists as CANCELLED. Only the durable logical Engine boundary may
		// reinterpret that transport status as a successful handoff.
		session, ok := terminalDelegatedExecutionSession(sessions, run.ID)
		if !ok || recoveredExecutionOutcome(session, events, "delegation_handoff", false) != recoveredExecutionSucceeded {
			continue
		}
		if !synchronized {
			safe, err := p.delegationRecoveryContext(ctx, run)
			if err != nil {
				return err
			}
			syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
			switch {
			case strings.TrimSpace(session.RunnerID) != "":
				err = p.syncWorkspaceFromRunner(syncCtx, safe, session.RunnerID, session.ID)
			case strings.TrimSpace(session.RuntimeInstanceID) != "":
				_, err = p.finalizeServerWorkspace(syncCtx, safe)
			default:
				err = fmt.Errorf("run execution: completed delegation session has no recoverable execution owner")
			}
			cancel()
			if err != nil {
				return err
			}
		}
		if err := recovery.MarkDelegationWorkspaceHandoffReady(
			ctx,
			delegation.ProjectID,
			delegation.ParentRunID,
			delegation.ID,
			delegation.DelegatedRunID,
		); err != nil {
			return err
		}
	}
	return nil
}

func listDelegationRecoveryEvents(ctx context.Context, recovery delegationRecoveryEventStore, projectID, runID string) ([]store.Event, error) {
	var result []store.Event
	after := int64(0)
	for {
		page, err := recovery.ListRunEvents(ctx, projectID, runID, after, delegationRecoveryEventPageSize)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return result, nil
		}
		for _, event := range page {
			if event.Sequence == nil || *event.Sequence <= after {
				return nil, fmt.Errorf("run execution: invalid delegation recovery event sequence")
			}
			after = *event.Sequence
			result = append(result, event)
		}
		if len(page) < delegationRecoveryEventPageSize {
			return result, nil
		}
	}
}

func delegationRecoveryEvidence(events []store.Event, delegation store.Delegation) (bool, bool) {
	completedSequence := int64(0)
	for _, event := range events {
		if event.Sequence == nil {
			continue
		}
		if event.Type == "tool.completed" {
			var payload evidence.ToolPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				continue
			}
			targetAgentID, _ := payload.Input["targetAgentId"].(string)
			task, _ := payload.Input["task"].(string)
			if payload.Name == "delegate_task" &&
				payload.ToolCallID == delegation.RequestKey &&
				targetAgentID == delegation.TargetAgentID &&
				task == delegation.Task {
				completedSequence = *event.Sequence
			}
			continue
		}
		if completedSequence == 0 || *event.Sequence <= completedSequence {
			continue
		}
		if authoritativeWorkspaceHandbackEvent(event) {
			return true, true
		}
	}
	return completedSequence != 0, false
}

func (p *Processor) delegationRecoveryContext(ctx context.Context, run store.Run) (executioncontext.SafeContext, error) {
	snapshot, err := p.store.GetRunProvenance(ctx, run.ProjectID, run.ID)
	if err != nil {
		return executioncontext.SafeContext{}, err
	}
	var provenance executioncontext.Provenance
	if err := json.Unmarshal(snapshot, &provenance); err != nil {
		return executioncontext.SafeContext{}, fmt.Errorf("run execution: decode delegation recovery provenance: %w", err)
	}
	safe := provenance.Context
	if provenance.SchemaVersion != executioncontext.ProvenanceSchemaVersion ||
		safe.Project.ID != run.ProjectID ||
		safe.Run.ID != run.ID ||
		safe.Issue.ID != run.IssueID ||
		safe.Workspace.ID != run.WorkspaceID ||
		run.AgentID == nil ||
		safe.Agent.ID != *run.AgentID {
		return executioncontext.SafeContext{}, fmt.Errorf("run execution: delegation recovery provenance does not match the claimed Run")
	}
	return safe, nil
}

func (p *Processor) cleanupRuntime(parent context.Context, safe executioncontext.SafeContext, instance store.RuntimeInstance) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), cleanupTimeout)
	defer cancel()
	var errs []error
	if stopped, err := p.runtimes.Stop(ctx, instance.ProjectID, instance.ID, runtimepkg.StopReasonRequested); err != nil {
		errs = append(errs, err)
	} else if stopped.ID != "" {
		if recordErr := p.record(ctx, safe, "runtime.stopped", nil, &instance.ID, nil); recordErr != nil {
			errs = append(errs, recordErr)
		}
	}
	if destroyed, err := p.runtimes.Destroy(ctx, instance.ProjectID, instance.ID); err != nil {
		errs = append(errs, err)
	} else if destroyed.ID != "" {
		if recordErr := p.record(ctx, safe, "runtime.destroyed", nil, &instance.ID, nil); recordErr != nil {
			errs = append(errs, recordErr)
		}
	}
	return errors.Join(errs...)
}

func (p *Processor) record(ctx context.Context, safe executioncontext.SafeContext, eventType string, payload any, runtimeInstanceID *string, parentEventID *string) error {
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return err
	}
	issueID, runID, agentID, workspaceID := safe.Issue.ID, safe.Run.ID, safe.Agent.ID, safe.Workspace.ID
	_, err = p.events.Record(ctx, store.Event{
		Type:              eventType,
		ProjectID:         safe.Project.ID,
		IssueID:           &issueID,
		RunID:             &runID,
		AgentID:           &agentID,
		WorkspaceID:       &workspaceID,
		RuntimeInstanceID: optionalEventID(derefID(runtimeInstanceID)),
		ParentEventID:     optionalEventID(derefID(parentEventID)),
		Actor:             store.EmptyObject,
		Payload:           encoded,
	})
	return err
}

func derefID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

func failed(err error) scheduler.Result {
	reason := safeFailure(err)
	return scheduler.Result{RunStatus: "FAILED", FailureReason: &reason}
}

func safeFailure(err error) string {
	if err == nil {
		return "execution failed"
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 1024 {
		message = message[:1024]
	}
	if message == "" {
		return "execution failed"
	}
	return message
}

var _ scheduler.Processor = (*Processor)(nil)
var _ scheduler.Reconciler = (*Processor)(nil)
var _ engine.ProcessLauncher = (*processLauncher)(nil)
var _ engine.ProcessAttacher = (*processLauncher)(nil)

type runnerSessionStarter interface {
	StartOnRunner(context.Context, string, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error)
	StartPreparedOnRunner(context.Context, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error)
	Attach(context.Context, string, string) (*app.AuthorizedExecutionProcess, error)
}

type processLauncher struct {
	sessions          SessionService
	events            *evidence.Recorder
	output            *evidence.OutputRecorder
	safe              executioncontext.SafeContext
	runtimeInstanceID string
	runnerID          string
	attachSessionID   string
	scope             evidence.RunScope
	branches          *branchObserver
}

func (l *processLauncher) Start(ctx context.Context, request engine.ProcessRequest) (engine.Process, error) {
	payload := processPayload(request, nil, nil)
	startedType := "tool.started"
	if request.Kind == "test" {
		startedType = "test.started"
	}
	started, err := l.record(ctx, startedType, payload, nil)
	if err != nil {
		return nil, err
	}
	var process *app.AuthorizedExecutionProcess
	if l.runnerID != "" {
		starter, ok := l.sessions.(runnerSessionStarter)
		if !ok {
			return nil, fmt.Errorf("run execution: runner session starter is unavailable")
		}
		authorizedRequest := app.AuthorizedExecutionRequest{
			Command:               append([]string(nil), request.Command...),
			CWD:                   request.CWD,
			Env:                   cloneMap(request.Env),
			ProviderCredentialEnv: request.ProviderCredentialEnv,
			RuntimeSecretRefs:     cloneMap(request.RuntimeSecretRefs),
		}
		if strings.TrimSpace(l.attachSessionID) != "" {
			process, err = starter.StartPreparedOnRunner(ctx, l.scope.ProjectID, l.attachSessionID, authorizedRequest)
		} else {
			process, err = starter.StartOnRunner(ctx, l.scope.ProjectID, l.scope.RunID, l.runnerID, authorizedRequest)
		}
	} else {
		process, err = l.sessions.Start(ctx, l.scope.ProjectID, l.scope.RunID, l.runtimeInstanceID, app.AuthorizedExecutionRequest{
			Command:               append([]string(nil), request.Command...),
			CWD:                   request.CWD,
			Env:                   cloneMap(request.Env),
			ProviderCredentialEnv: request.ProviderCredentialEnv,
			RuntimeSecretRefs:     cloneMap(request.RuntimeSecretRefs),
		})
	}
	if err != nil {
		_ = l.recordFailure(ctx, request, &started.ID, nil, err)
		return nil, err
	}
	return newCapturingProcess(ctx, process, l, request, started.ID), nil
}

func (l *processLauncher) Attach(ctx context.Context) (engine.Process, error) {
	if l == nil || strings.TrimSpace(l.attachSessionID) == "" {
		return nil, engine.ErrNotAttachable
	}
	process, err := l.sessions.Attach(ctx, l.scope.ProjectID, l.attachSessionID)
	if err != nil {
		if isNotRunningExecutionSession(err) {
			return nil, engine.ErrNotAttachable
		}
		return nil, err
	}
	if process == nil {
		return nil, fmt.Errorf("run execution: attached Execution Session is unavailable")
	}
	return newCapturingProcess(ctx, process, l, engine.ProcessRequest{Kind: "tool", Name: "attached"}, ""), nil
}

func isNotRunningExecutionSession(err error) bool {
	apiErr, ok := app.AsError(err)
	return ok && apiErr.Code == "execution_session_not_running"
}

func (l *processLauncher) observeBranchAfterTerminal(ctx context.Context, eventType string) {
	if shouldObserveBranchAfterActivity(eventType) {
		l.branches.observeIfChanged(ctx, l.safe, &l.runtimeInstanceID)
	}
}

func (l *processLauncher) record(ctx context.Context, eventType string, payload any, parent *string) (store.Event, error) {
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return store.Event{}, err
	}
	issueID, runID, agentID, workspaceID := l.safe.Issue.ID, l.safe.Run.ID, l.safe.Agent.ID, l.safe.Workspace.ID
	return l.events.Record(ctx, store.Event{Type: eventType, ProjectID: l.safe.Project.ID, IssueID: &issueID, RunID: &runID, AgentID: &agentID, WorkspaceID: &workspaceID, RuntimeInstanceID: optionalEventID(l.runtimeInstanceID), ParentEventID: parent, Actor: store.EmptyObject, Payload: encoded})
}

func (l *processLauncher) recordFailure(ctx context.Context, request engine.ProcessRequest, parent *string, chunks []store.RawOutputChunk, cause error) error {
	payload := processPayload(request, nil, chunkIDs(chunks))
	eventType := "tool.failed"
	if request.Kind == "test" {
		eventType = "test.failed"
		payload = evidence.TestPayload{Command: append([]string(nil), request.Command...), Status: "failed", OutputChunkIDs: chunkIDs(chunks)}
	}
	_, err := l.record(ctx, eventType, flattenFailurePayload(payload, cause), parent)
	return err
}

func flattenFailurePayload(payload any, cause error) any {
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return map[string]any{"reason": safeFailure(cause)}
	}
	var mapped map[string]any
	if err := json.Unmarshal(encoded, &mapped); err != nil {
		return map[string]any{"reason": safeFailure(cause)}
	}
	if mapped == nil {
		mapped = map[string]any{}
	}
	mapped["reason"] = safeFailure(cause)
	return mapped
}

func processPayload(request engine.ProcessRequest, exitCode *int, outputChunkIDs []string) any {
	if request.Kind == "test" {
		status := "running"
		if exitCode != nil {
			if *exitCode == 0 {
				status = "passed"
			} else {
				status = "failed"
			}
		}
		return evidence.TestPayload{Command: append([]string(nil), request.Command...), Status: status, ExitCode: exitCode, OutputChunkIDs: outputChunkIDs}
	}
	return evidence.ToolPayload{Kind: request.Kind, Name: request.Name, Command: append([]string(nil), request.Command...), CWD: request.CWD, ExitCode: exitCode, OutputChunkIDs: outputChunkIDs}
}

func cloneMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func optionalEventID(id string) *string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return &id
}

type captureResult struct {
	chunks []store.RawOutputChunk
	err    error
}

type captureStream struct {
	reader  *io.PipeReader
	claimed atomic.Bool
}

func (s *captureStream) Reader() io.Reader {
	s.claimed.Store(true)
	return s.reader
}

func (s *captureStream) CloseIfUnclaimed() {
	if !s.claimed.Load() {
		_ = s.reader.Close()
	}
}

func (s *captureStream) Close() {
	_ = s.reader.Close()
}

type engineStreamWriter struct {
	writer *io.PipeWriter
}

func (w engineStreamWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	if errors.Is(err, io.ErrClosedPipe) {
		return len(data), nil
	}
	return written, err
}

type capturingProcess struct {
	process       *app.AuthorizedExecutionProcess
	launcher      *processLauncher
	request       engine.ProcessRequest
	parentEventID string
	stdout        *captureStream
	stderr        *captureStream
	stdoutDone    <-chan captureResult
	stderrDone    <-chan captureResult
	waitOnce      sync.Once
	waitResult    engine.ProcessResult
	waitErr       error
	stopRequested atomic.Bool
}

func newCapturingProcess(ctx context.Context, process *app.AuthorizedExecutionProcess, launcher *processLauncher, request engine.ProcessRequest, parentEventID string) *capturingProcess {
	stdout, stdoutDone := startCapture(ctx, launcher.output, launcher.scope, "STDOUT", process.Stdout())
	stderr, stderrDone := startCapture(ctx, launcher.output, launcher.scope, "STDERR", process.Stderr())
	return &capturingProcess{process: process, launcher: launcher, request: request, parentEventID: parentEventID, stdout: stdout, stderr: stderr, stdoutDone: stdoutDone, stderrDone: stderrDone}
}

func startCapture(ctx context.Context, recorder *evidence.OutputRecorder, scope evidence.RunScope, stream string, source io.Reader) (*captureStream, <-chan captureResult) {
	reader, writer := io.Pipe()
	streamReader := &captureStream{reader: reader}
	done := make(chan captureResult, 1)
	go func() {
		chunks, err := recorder.Capture(ctx, scope, stream, io.TeeReader(source, engineStreamWriter{writer: writer}))
		_ = writer.CloseWithError(err)
		done <- captureResult{chunks: chunks, err: err}
		close(done)
	}()
	return streamReader, done
}

func (p *capturingProcess) ID() string { return p.process.ID() }
func (p *capturingProcess) WorkingDirectory() string {
	if p == nil || p.process == nil {
		return ""
	}
	return p.process.WorkingDirectory()
}
func (p *capturingProcess) Stdout() io.Reader     { return p.stdout.Reader() }
func (p *capturingProcess) Stderr() io.Reader     { return p.stderr.Reader() }
func (p *capturingProcess) Stdin() io.WriteCloser { return p.process.Stdin() }
func (p *capturingProcess) Terminate(ctx context.Context) error {
	p.stopRequested.Store(true)
	return p.process.Terminate(ctx)
}
func (p *capturingProcess) Kill(ctx context.Context) error {
	p.stopRequested.Store(true)
	return p.process.Kill(ctx)
}

func (p *capturingProcess) Wait(ctx context.Context) (engine.ProcessResult, error) {
	p.waitOnce.Do(func() {
		// If an Engine never requested a stream, detach that presentation pipe
		// before waiting. Evidence capture continues because closed presentation
		// pipes are ignored by engineStreamWriter while the source is still read.
		p.stdout.CloseIfUnclaimed()
		p.stderr.CloseIfUnclaimed()
		result, waitErr := p.process.Wait(ctx)
		if waitErr != nil && ctx.Err() != nil {
			p.stdout.Close()
			p.stderr.Close()
			p.releaseCancelledTransport(ctx)
		}
		stdout := <-p.stdoutDone
		stderr := <-p.stderrDone
		chunks := append(append([]store.RawOutputChunk(nil), stdout.chunks...), stderr.chunks...)
		captureErr := errors.Join(stdout.err, stderr.err)
		p.waitResult = engine.ProcessResult{ExitCode: result.ExitCode}
		p.waitErr = errors.Join(waitErr, captureErr)
		exitCode := result.ExitCode
		parent := optionalEventID(p.parentEventID)
		terminalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), processCancellationCleanupTimeout)
		defer cancel()
		if p.waitErr == nil && result.ExitCode != 0 && p.stopRequested.Load() && p.request.Kind != "test" {
			if _, eventErr := p.launcher.record(terminalCtx, "tool.stopped", processPayload(p.request, &exitCode, chunkIDs(chunks)), parent); eventErr != nil {
				p.waitErr = eventErr
			} else {
				p.launcher.observeBranchAfterTerminal(terminalCtx, "tool.stopped")
			}
			return
		}
		if p.waitErr != nil || result.ExitCode != 0 {
			cause := p.waitErr
			if cause == nil {
				cause = fmt.Errorf("process exited with code %d", result.ExitCode)
			}
			if eventErr := p.launcher.recordFailure(terminalCtx, p.request, parent, chunks, cause); eventErr != nil {
				p.waitErr = errors.Join(p.waitErr, eventErr)
			} else {
				eventType := "tool.failed"
				if p.request.Kind == "test" {
					eventType = "test.failed"
				}
				p.launcher.observeBranchAfterTerminal(terminalCtx, eventType)
			}
			return
		}
		eventType := "tool.completed"
		payload := processPayload(p.request, &exitCode, chunkIDs(chunks))
		if p.request.Kind == "test" {
			eventType = "test.completed"
		}
		if _, eventErr := p.launcher.record(terminalCtx, eventType, payload, parent); eventErr != nil {
			p.waitErr = eventErr
		} else {
			p.launcher.observeBranchAfterTerminal(terminalCtx, eventType)
		}
	})
	return p.waitResult, p.waitErr
}

func (p *capturingProcess) releaseCancelledTransport(parent context.Context) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), processCancellationCleanupTimeout)
	defer cancel()
	_ = p.process.Terminate(cleanupCtx)
	_ = p.process.Kill(cleanupCtx)
}

func chunkIDs(chunks []store.RawOutputChunk) []string {
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	return ids
}

func (p *Processor) ensureRunnerWorkspace(ctx context.Context, run store.Run, safe executioncontext.SafeContext) (executioncontext.SafeContext, error) {
	if p.workspaces == nil {
		if strings.HasPrefix(strings.TrimSpace(safe.Workspace.Path), "pending://") || safe.Workspace.BootstrapStatus != "READY" {
			return executioncontext.SafeContext{}, fmt.Errorf("authoritative Issue Workspace is not ready for runner transfer")
		}
		return safe, nil
	}
	workspace, err := p.workspaces.EnsureIssueWorkspace(ctx, run.ProjectID, run.IssueID)
	if err != nil {
		return executioncontext.SafeContext{}, err
	}
	if workspace.BootstrapStatus != "READY" || strings.HasPrefix(strings.TrimSpace(workspace.Path), "pending://") {
		return executioncontext.SafeContext{}, fmt.Errorf("authoritative Issue Workspace is not ready for runner transfer")
	}
	materialized, err := p.resolver.Resolve(ctx, run.ProjectID, run.ID)
	if err != nil {
		return executioncontext.SafeContext{}, err
	}
	if materialized.Safe.Workspace.ID != workspace.ID || materialized.Safe.Workspace.Path != workspace.Path {
		return executioncontext.SafeContext{}, fmt.Errorf("run execution: workspace bindings changed during runner materialization")
	}
	return materialized.Safe, nil
}

func (p *Processor) prepareRunnerSessionWorkspace(ctx context.Context, run store.Run, safe executioncontext.SafeContext, runnerID, sessionID string) (executioncontext.SafeContext, error) {
	safe = p.attachRunnerProvenance(ctx, safe, runnerID)
	if err := executioncontext.EnsureProvenance(ctx, p.store, run.ProjectID, run.ID, safe); err != nil {
		return executioncontext.SafeContext{}, err
	}
	if err := p.transferWorkspaceToRunner(ctx, safe, runnerID, sessionID); err != nil {
		return executioncontext.SafeContext{}, err
	}
	return safe, nil
}

func (p *Processor) transferWorkspaceToRunner(ctx context.Context, safe executioncontext.SafeContext, runnerID, sessionID string) error {
	if isRemoteGitProject(safe) {
		return p.prepareRemoteGitWorkspace(ctx, safe, runnerID, sessionID)
	}
	if p.runners == nil {
		return fmt.Errorf("runner connector is unavailable")
	}
	gitCLI, ok := p.git.(*workspace.GitCLI)
	if !ok {
		return fmt.Errorf("workspace git transfer is unavailable")
	}
	locker, ok := p.store.(store.WorkspaceExecutionLockStore)
	if !ok {
		return fmt.Errorf("workspace execution lock store is unavailable")
	}
	transferID := fmt.Sprintf("%s-%d", safe.Run.ID, time.Now().UnixNano())
	if err := p.record(ctx, safe, "workspace.transfer.started", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", nil), nil, nil); err != nil {
		return err
	}
	lock, err := locker.AcquireWorkspaceExecutionLock(ctx, safe.Workspace.ID, sessionID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	defer func() { _ = lock.Release() }()
	payload, err := gitCLI.TransferSnapshot(ctx, safe.Workspace.Path, transferID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	client, err := p.runners.Connect(ctx, safe.Project.ID, runnerID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	progress := p.transferProgressRecorder(ctx, safe, runnerID, transferID, "to_runner")
	if err := client.SendTransfer(ctx, sessionID, transferID, "to_runner", payload, progress); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	return p.record(ctx, safe, "workspace.transfer.completed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{
		"bytesTransferred": len(payload), "totalBytes": len(payload),
	}), nil, nil)
}

func (p *Processor) runEngineOnRunner(ctx context.Context, run store.Run, safe executioncontext.SafeContext, runnerID, attachSessionID string) (scheduler.Result, error) {
	return p.runEngineOnRunnerWithContinuation(ctx, run, safe, runnerID, attachSessionID, nil)
}

func (p *Processor) runEngineOnRunnerWithContinuation(ctx context.Context, run store.Run, safe executioncontext.SafeContext, runnerID, attachSessionID string, delegationContinuation *engine.DelegationContinuation) (scheduler.Result, error) {
	adapter, err := p.engines.Get(safe.Agent.Engine)
	if err != nil {
		return failed(err), nil
	}
	launcher := &processLauncher{
		sessions: p.sessions, events: p.events, output: p.output, safe: safe,
		runnerID: runnerID, attachSessionID: attachSessionID,
		scope:    evidence.RunScope{ProjectID: run.ProjectID, IssueID: run.IssueID, RunID: run.ID},
		branches: p.branches,
	}
	request, err := p.engineRequestWithDelegationContinuation(ctx, safe, launcher, "", delegationContinuation)
	if err != nil {
		return failed(err), nil
	}
	engineResult, engineErr := adapter.Execute(ctx, request)
	if errors.Is(engineErr, engine.ErrWaitingForInput) {
		return p.finishWaitingForInputRunner(ctx, safe)
	}
	handoff, handoffRequested := engine.AsDelegationHandoff(engineErr)
	if engineErr != nil && !handoffRequested {
		if uncertaintyErr := p.executionUncertainty(ctx, run, engineErr); uncertaintyErr != nil {
			return scheduler.Result{}, uncertaintyErr
		}
	}
	if engineErr == nil || handoffRequested {
		boundary := "completed"
		if handoffRequested {
			boundary = "delegation_handoff"
		}
		if err := p.record(ctx, safe, "engine.execution.completed", map[string]any{"boundary": boundary}, nil, nil); err != nil {
			return p.failExecution(ctx, safe, err, nil)
		}
	}

	syncCtx, cancelSync := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	syncErr := p.syncWorkspaceFromRunner(syncCtx, safe, runnerID, attachSessionID)
	if syncErr == nil {
		syncErr = p.recordDelegationWorkspaceAccepted(syncCtx, safe, nil)
	}
	cancelSync()
	if ctx.Err() != nil {
		return scheduler.Result{}, ctx.Err()
	}
	if handoffRequested {
		if syncErr != nil {
			return p.failExecution(ctx, safe, syncErr, nil)
		}
		return p.finishDelegationWorkspaceHandoff(ctx, safe, handoff, nil)
	}
	if combined := errors.Join(engineErr, syncErr); combined != nil {
		return p.failExecution(ctx, safe, combined, nil)
	}
	if engineResult.Summary != "" {
		if err := p.record(ctx, safe, "agent.message", map[string]any{"message": engineResult.Summary}, nil, nil); err != nil {
			return scheduler.Result{}, err
		}
	}
	return p.finishSuccessfulExecution(ctx, safe, nil)
}

func (p *Processor) recordDelegationWorkspaceAccepted(ctx context.Context, safe executioncontext.SafeContext, runtimeInstanceID *string) error {
	if safe.Delegation == nil {
		return nil
	}
	return p.record(ctx, safe, "delegation.workspace_accepted", map[string]any{
		"delegationId": safe.Delegation.ID,
	}, runtimeInstanceID, nil)
}

func (p *Processor) finishSuccessfulExecution(ctx context.Context, safe executioncontext.SafeContext, runtimeInstanceID *string) (scheduler.Result, error) {
	if safe.Delegation != nil {
		return scheduler.Result{RunStatus: "COMPLETED"}, nil
	}
	if err := p.record(ctx, safe, "run.ready_for_review", map[string]any{"codeState": "git"}, runtimeInstanceID, nil); err != nil {
		return scheduler.Result{}, err
	}
	return scheduler.Result{RunStatus: "READY_FOR_REVIEW"}, nil
}

func (p *Processor) transferProgressRecorder(ctx context.Context, safe executioncontext.SafeContext, runnerID, transferID, direction string) runner.TransferProgressFunc {
	return func(progress runner.TransferProgress) {
		_ = p.record(ctx, safe, "workspace.transfer.progress", p.transferEventPayload(ctx, runnerID, transferID, direction, map[string]any{
			"bytesTransferred": progress.BytesTransferred, "totalBytes": progress.TotalBytes,
		}), nil, nil)
	}
}

func (p *Processor) syncWorkspaceFromRunner(ctx context.Context, safe executioncontext.SafeContext, runnerID, sessionID string) error {
	if isRemoteGitProject(safe) {
		return p.publishRemoteGitWorkspace(ctx, safe, runnerID, sessionID)
	}
	if sessionID == "" || p.runners == nil {
		return fmt.Errorf("workspace sync requires a prepared runner execution session")
	}
	gitCLI, ok := p.git.(*workspace.GitCLI)
	if !ok {
		return fmt.Errorf("workspace git transfer is unavailable")
	}
	locker, ok := p.store.(store.WorkspaceExecutionLockStore)
	if !ok {
		return fmt.Errorf("workspace execution lock store is unavailable")
	}
	transferID := fmt.Sprintf("%s-sync-%d", sessionID, time.Now().UnixNano())
	if err := p.record(ctx, safe, "workspace.transfer.started", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", nil), nil, nil); err != nil {
		return err
	}
	client, err := p.runners.Connect(ctx, safe.Project.ID, runnerID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	progress := p.transferProgressRecorder(ctx, safe, runnerID, transferID, "from_runner")
	if err := client.SendTransfer(ctx, sessionID, transferID, "from_runner", nil, nil); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	receivedID, payload, err := client.ReceiveTransfer(ctx, sessionID, progress)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	if receivedID != "" {
		transferID = receivedID
	}
	lock, err := locker.AcquireWorkspaceExecutionLock(ctx, safe.Workspace.ID, sessionID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	defer func() { _ = lock.Release() }()
	if err := gitCLI.ApplyTransferBundle(ctx, safe.Workspace.Path, payload); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	if err := p.persistLocalWorkspaceRevision(ctx, safe); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	// Persist the authoritative hand-back boundary before acknowledging Runner
	// cleanup. If the acknowledgement is lost, reconciliation can trust this
	// server-side evidence while the Runner safely retains its recovery copy.
	if err := p.record(ctx, safe, "workspace.transfer.completed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{
		"bytesTransferred": len(payload), "totalBytes": len(payload),
	}), nil, nil); err != nil {
		return err
	}
	if err := client.ConfirmTransferApplied(ctx, sessionID, transferID); err != nil {
		wrapped := fmt.Errorf("acknowledge applied workspace transfer: %w", err)
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": wrapped.Error()}), nil, nil)
		return wrapped
	}
	return nil
}

func (p *Processor) finishWaitingForInputRunner(ctx context.Context, safe executioncontext.SafeContext) (scheduler.Result, error) {
	if !store.SupportsQuestionStore(p.store) {
		return failed(fmt.Errorf("run execution: Question store capability is required for WAITING_FOR_INPUT")), nil
	}
	questions := any(p.store).(store.QuestionStore)
	question, err := questions.GetOpenBlockingQuestion(ctx, safe.Project.ID, safe.Run.ID)
	if err != nil {
		if ctx.Err() != nil {
			return scheduler.Result{}, ctx.Err()
		}
		return failed(fmt.Errorf("run execution: persisted blocking Question is required before WAITING_FOR_INPUT: %w", err)), nil
	}
	_ = p.record(ctx, safe, "run.waiting_for_input", map[string]any{"questionId": question.ID}, nil, nil)
	return scheduler.Result{RunStatus: "WAITING_FOR_INPUT"}, nil
}

type runnerLookup interface {
	GetRunner(context.Context, string) (store.Runner, error)
}

func (p *Processor) attachRunnerProvenance(ctx context.Context, safe executioncontext.SafeContext, runnerID string) executioncontext.SafeContext {
	runner := &executioncontext.RunnerContext{ID: runnerID}
	if lookup, ok := p.store.(runnerLookup); ok {
		if record, err := lookup.GetRunner(ctx, runnerID); err == nil {
			runner.Name = record.Name
			runner.Internal = record.Internal
		}
	}
	safe.Runner = runner
	return safe
}

func (p *Processor) transferEventPayload(ctx context.Context, runnerID, transferID, direction string, extra map[string]any) map[string]any {
	payload := map[string]any{"direction": direction, "runnerId": runnerID, "transferId": transferID}
	if lookup, ok := p.store.(runnerLookup); ok {
		if record, err := lookup.GetRunner(ctx, runnerID); err == nil && record.Name != "" {
			payload["runnerName"] = record.Name
		}
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}
