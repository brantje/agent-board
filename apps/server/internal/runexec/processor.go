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

type SessionService interface {
	Attach(context.Context, string, string) (*app.AuthorizedExecutionProcess, error)
	ReconcileAll(context.Context) error
}

type runRedactionReleaser interface {
	ReleaseRunRedaction(string)
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
		store: store, resolver: resolver, runners: runners, sessions: sessions, engines: engines,
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
	live, err := p.liveExecutionSession(ctx, run)
	if err != nil {
		return failed(err), nil
	}
	if live != nil {
		return p.attachExistingExecution(ctx, run, resolved.Safe, *live)
	}
	return p.startNewExecution(ctx, claim, run, resolved.Safe)
}

func (p *Processor) startNewExecution(ctx context.Context, claim *store.SchedulerAdmission, run store.Run, safe executioncontext.SafeContext) (scheduler.Result, error) {
	runEventType := "run.started"
	if claim.Job.Kind == "RESUME" {
		runEventType = "run.resumed"
	}
	if err := p.record(ctx, safe, runEventType, nil, nil); err != nil {
		return scheduler.Result{}, err
	}
	p.branches.observeIfChanged(ctx, safe)

	runnerID := strings.TrimSpace(claim.RunnerID)
	if runnerID == "" {
		return failed(fmt.Errorf("scheduler admission is missing runner id")), nil
	}
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
	safe = p.attachRunnerProvenance(ctx, safe, runnerID)
	if err := executioncontext.EnsureProvenance(ctx, p.store, run.ProjectID, run.ID, safe); err != nil {
		return failed(err), nil
	}
	if err := p.transferWorkspaceToRunner(ctx, safe, runnerID, session.ID); err != nil {
		return failed(err), nil
	}
	return p.runEngineOnRunner(ctx, run, safe, runnerID, session.ID)
}

func (p *Processor) attachExistingExecution(ctx context.Context, run store.Run, safe executioncontext.SafeContext, session store.ExecutionSession) (scheduler.Result, error) {
	if err := p.record(ctx, safe, "run.resumed", map[string]any{"reason": "lease_reconciliation"}, nil); err != nil {
		return scheduler.Result{}, err
	}
	p.branches.observeIfChanged(ctx, safe)
	runnerID := strings.TrimSpace(session.RunnerID)
	if runnerID == "" {
		return failed(fmt.Errorf("execution session is missing runner id")), nil
	}
	safe = p.attachRunnerProvenance(ctx, safe, runnerID)
	return p.runEngineOnRunner(ctx, run, safe, runnerID, session.ID)
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
	// enough proof that replaying the Engine is safe. Keep ownership uncertain
	// rather than blindly duplicating Workspace side effects.
	return store.SchedulerReconciliationUnknown, nil, nil
}

func (p *Processor) record(ctx context.Context, safe executioncontext.SafeContext, eventType string, payload any, parentEventID *string) error {
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return err
	}
	issueID, runID, agentID, workspaceID := safe.Issue.ID, safe.Run.ID, safe.Agent.ID, safe.Workspace.ID
	_, err = p.events.Record(ctx, store.Event{
		Type:          eventType,
		ProjectID:     safe.Project.ID,
		IssueID:       &issueID,
		RunID:         &runID,
		AgentID:       &agentID,
		WorkspaceID:   &workspaceID,
		ParentEventID: optionalEventID(derefID(parentEventID)),
		Actor:         store.EmptyObject,
		Payload:       encoded,
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
	sessions        SessionService
	events          *evidence.Recorder
	output          *evidence.OutputRecorder
	safe            executioncontext.SafeContext
	runnerID        string
	attachSessionID string
	scope           evidence.RunScope
	branches        *branchObserver
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
	starter, ok := l.sessions.(runnerSessionStarter)
	if !ok {
		return nil, fmt.Errorf("run execution: runner session starter is unavailable")
	}
	if strings.TrimSpace(l.runnerID) == "" {
		return nil, fmt.Errorf("run execution: runner id is required")
	}
	authorizedRequest := app.AuthorizedExecutionRequest{
		Command:               append([]string(nil), request.Command...),
		CWD:                   request.CWD,
		Env:                   cloneMap(request.Env),
		ProviderCredentialEnv: request.ProviderCredentialEnv,
	}
	var process *app.AuthorizedExecutionProcess
	if strings.TrimSpace(l.attachSessionID) != "" {
		process, err = starter.StartPreparedOnRunner(ctx, l.scope.ProjectID, l.attachSessionID, authorizedRequest)
	} else {
		process, err = starter.StartOnRunner(ctx, l.scope.ProjectID, l.scope.RunID, l.runnerID, authorizedRequest)
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
		l.branches.observeIfChanged(ctx, l.safe)
	}
}

func (l *processLauncher) record(ctx context.Context, eventType string, payload any, parent *string) (store.Event, error) {
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return store.Event{}, err
	}
	issueID, runID, agentID, workspaceID := l.safe.Issue.ID, l.safe.Run.ID, l.safe.Agent.ID, l.safe.Workspace.ID
	return l.events.Record(ctx, store.Event{Type: eventType, ProjectID: l.safe.Project.ID, IssueID: &issueID, RunID: &runID, AgentID: &agentID, WorkspaceID: &workspaceID, ParentEventID: parent, Actor: store.EmptyObject, Payload: encoded})
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

func (p *capturingProcess) ID() string            { return p.process.ID() }
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
	if err := p.record(ctx, safe, "workspace.transfer.started", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", nil), nil); err != nil {
		return err
	}
	lock, err := locker.AcquireWorkspaceExecutionLock(ctx, safe.Workspace.ID, sessionID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	defer func() { _ = lock.Release() }()
	payload, err := gitCLI.TransferSnapshot(ctx, safe.Workspace.Path, transferID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	client, err := p.runners.Connect(ctx, safe.Project.ID, runnerID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	progress := p.transferProgressRecorder(ctx, safe, runnerID, transferID, "to_runner")
	if err := client.SendTransfer(ctx, sessionID, transferID, "to_runner", payload, progress); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	return p.record(ctx, safe, "workspace.transfer.completed", p.transferEventPayload(ctx, runnerID, transferID, "to_runner", map[string]any{
		"bytesTransferred": len(payload), "totalBytes": len(payload),
	}), nil)
}

func (p *Processor) runEngineOnRunner(ctx context.Context, run store.Run, safe executioncontext.SafeContext, runnerID, attachSessionID string) (scheduler.Result, error) {
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
	request, err := p.engineRequest(ctx, safe, launcher)
	if err != nil {
		return failed(err), nil
	}
	engineResult, engineErr := adapter.Execute(ctx, request)
	if errors.Is(engineErr, engine.ErrWaitingForInput) {
		return p.finishWaitingForInputRunner(ctx, safe)
	}

	syncCtx, cancelSync := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	syncErr := p.syncWorkspaceFromRunner(syncCtx, safe, runnerID, attachSessionID)
	cancelSync()
	if ctx.Err() != nil {
		return scheduler.Result{}, ctx.Err()
	}
	if combined := errors.Join(engineErr, syncErr); combined != nil {
		reason := safeFailure(combined)
		_ = p.record(ctx, safe, "run.failed", map[string]any{"reason": reason}, nil)
		return scheduler.Result{RunStatus: "FAILED", FailureReason: &reason}, nil
	}
	if engineResult.Summary != "" {
		if err := p.record(ctx, safe, "agent.message", map[string]any{"message": engineResult.Summary}, nil); err != nil {
			return scheduler.Result{}, err
		}
	}
	if err := p.record(ctx, safe, "run.ready_for_review", map[string]any{"codeState": "git"}, nil); err != nil {
		return scheduler.Result{}, err
	}
	return scheduler.Result{RunStatus: "READY_FOR_REVIEW"}, nil
}

func (p *Processor) transferProgressRecorder(ctx context.Context, safe executioncontext.SafeContext, runnerID, transferID, direction string) runner.TransferProgressFunc {
	return func(progress runner.TransferProgress) {
		_ = p.record(ctx, safe, "workspace.transfer.progress", p.transferEventPayload(ctx, runnerID, transferID, direction, map[string]any{
			"bytesTransferred": progress.BytesTransferred, "totalBytes": progress.TotalBytes,
		}), nil)
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
	if err := p.record(ctx, safe, "workspace.transfer.started", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", nil), nil); err != nil {
		return err
	}
	client, err := p.runners.Connect(ctx, safe.Project.ID, runnerID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	progress := p.transferProgressRecorder(ctx, safe, runnerID, transferID, "from_runner")
	if err := client.SendTransfer(ctx, sessionID, transferID, "from_runner", nil, nil); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	receivedID, payload, err := client.ReceiveTransfer(ctx, sessionID, progress)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	if receivedID != "" {
		transferID = receivedID
	}
	lock, err := locker.AcquireWorkspaceExecutionLock(ctx, safe.Workspace.ID, sessionID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	defer func() { _ = lock.Release() }()
	if err := gitCLI.ApplyTransferBundle(ctx, safe.Workspace.Path, payload); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	if err := p.persistLocalWorkspaceRevision(ctx, safe); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": err.Error()}), nil)
		return err
	}
	if err := client.ConfirmTransferApplied(ctx, sessionID, transferID); err != nil {
		wrapped := fmt.Errorf("acknowledge applied workspace transfer: %w", err)
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{"reason": wrapped.Error()}), nil)
		return wrapped
	}
	return p.record(ctx, safe, "workspace.transfer.completed", p.transferEventPayload(ctx, runnerID, transferID, "from_runner", map[string]any{
		"bytesTransferred": len(payload), "totalBytes": len(payload),
	}), nil)
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
	_ = p.record(ctx, safe, "run.waiting_for_input", map[string]any{"questionId": question.ID}, nil)
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
