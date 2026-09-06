package runexec

import (
	"context"
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
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
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
}

type SessionService interface {
	Start(context.Context, string, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error)
	ReconcileAll(context.Context) error
}

type RuntimeService interface {
	Create(context.Context, string, string, string) (store.RuntimeInstance, error)
	Start(context.Context, string, string) (store.RuntimeInstance, error)
	Stop(context.Context, string, string, runtimepkg.StopReason) (store.RuntimeInstance, error)
	Destroy(context.Context, string, string) (store.RuntimeInstance, error)
}

type Processor struct {
	store     ExecutionStore
	resolver  ContextResolver
	runtimes  RuntimeService
	sessions  SessionService
	engines   *engine.Registry
	events    *evidence.Recorder
	output    *evidence.OutputRecorder
	candidate *evidence.CandidateSnapshotter
}

func NewProcessor(
	store ExecutionStore,
	resolver ContextResolver,
	runtimes RuntimeService,
	sessions SessionService,
	engines *engine.Registry,
	events *evidence.Recorder,
	output *evidence.OutputRecorder,
	candidate *evidence.CandidateSnapshotter,
) (*Processor, error) {
	if store == nil || resolver == nil || runtimes == nil || sessions == nil || engines == nil || events == nil || output == nil || candidate == nil {
		return nil, fmt.Errorf("run execution: all processor dependencies are required")
	}
	return &Processor{store: store, resolver: resolver, runtimes: runtimes, sessions: sessions, engines: engines, events: events, output: output, candidate: candidate}, nil
}

func (p *Processor) Process(ctx context.Context, claim *store.SchedulerAdmission, lifecycle scheduler.Lifecycle) (scheduler.Result, error) {
	if claim == nil || lifecycle == nil {
		return scheduler.Result{}, fmt.Errorf("run execution: scheduler admission and lifecycle are required")
	}
	run, err := lifecycle.Running(ctx)
	if err != nil {
		return scheduler.Result{}, err
	}
	resolved, err := p.resolver.Resolve(ctx, run.ProjectID, run.ID)
	if err != nil {
		return failed(err), nil
	}
	safe := resolved.Safe
	runEventType := "run.started"
	if claim.Job.Kind == "RESUME" {
		runEventType = "run.resumed"
	}
	if err := p.record(ctx, safe, runEventType, nil, nil, nil); err != nil {
		return scheduler.Result{}, err
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
	instance = started

	// Runtime creation is also the boundary that materializes a placeholder
	// Issue Workspace. Resolve again after that boundary so immutable provenance,
	// Engine context and candidate collection all refer to the same durable
	// Workspace that is actually mounted into the Runtime.
	materialized, err := p.resolver.Resolve(ctx, run.ProjectID, run.ID)
	if err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(err, cleanupErr)), nil
	}
	if materialized.Safe.Runtime.ID != safe.Runtime.ID || materialized.Safe.Workspace.ID != safe.Workspace.ID {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(fmt.Errorf("run execution: execution bindings changed during Runtime provisioning"), cleanupErr)), nil
	}
	safe = materialized.Safe
	if err := executioncontext.EnsureProvenance(ctx, p.store, run.ProjectID, run.ID, safe); err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return scheduler.Result{}, errors.Join(err, cleanupErr)
	}
	if err := p.record(ctx, safe, "runtime.started", map[string]any{"runtimeId": safe.Runtime.ID}, &instance.ID, nil); err != nil {
		_ = p.cleanupRuntime(ctx, safe, instance)
		return scheduler.Result{}, err
	}

	adapter, err := p.engines.Get(safe.Executor.Engine)
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
		scope:             evidence.RunScope{ProjectID: run.ProjectID, IssueID: run.IssueID, RunID: run.ID},
	}
	request, err := p.engineRequest(ctx, safe, launcher, instance.ID)
	if err != nil {
		cleanupErr := p.cleanupRuntime(ctx, safe, instance)
		return failed(errors.Join(err, cleanupErr)), nil
	}
	engineResult, engineErr := adapter.Execute(ctx, request)
	if errors.Is(engineErr, engine.ErrWaitingForInput) {
		return p.finishWaitingForInput(ctx, safe, instance)
	}

	snapshot, snapshotErr := p.candidate.Snapshot(ctx, launcher.scope, safe.Workspace.Path)
	if snapshotErr == nil {
		snapshotErr = p.recordCandidate(ctx, safe, instance.ID, snapshot)
	}
	if engineErr == nil && snapshotErr == nil && engineResult.Summary != "" {
		engineErr = p.record(ctx, safe, "agent.message", map[string]any{"message": engineResult.Summary}, &instance.ID, nil)
	}

	cleanupErr := p.cleanupRuntime(ctx, safe, instance)
	if ctx.Err() != nil {
		return scheduler.Result{}, ctx.Err()
	}
	if combined := errors.Join(engineErr, snapshotErr, cleanupErr); combined != nil {
		reason := safeFailure(combined)
		_ = p.record(ctx, safe, "run.failed", map[string]any{"reason": reason}, &instance.ID, nil)
		return scheduler.Result{RunStatus: "FAILED", FailureReason: &reason}, nil
	}
	if err := p.record(ctx, safe, "run.ready_for_review", map[string]any{"candidateManifestArtifactId": snapshot.Manifest.ID}, &instance.ID, nil); err != nil {
		return scheduler.Result{}, err
	}
	return scheduler.Result{RunStatus: "READY_FOR_REVIEW"}, nil
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

func (p *Processor) recordCandidate(ctx context.Context, safe executioncontext.SafeContext, runtimeInstanceID string, snapshot evidence.CandidateSnapshot) error {
	artifacts := append([]store.Artifact{snapshot.Manifest}, snapshot.Artifacts...)
	fileArtifacts := make(map[string]string)
	for _, artifact := range artifacts {
		if artifact.Kind == "candidate_file" {
			fileArtifacts[artifact.Name] = artifact.ID
		}
		if err := p.record(ctx, safe, "artifact.created", evidence.ArtifactPayload{ArtifactID: artifact.ID, Name: artifact.Name, Kind: artifact.Kind}, &runtimeInstanceID, nil); err != nil {
			return err
		}
	}
	for _, change := range snapshot.Candidate.Changes {
		eventType := candidateEventType(change)
		payload := evidence.FilePayload{Path: change.Path, OldPath: change.OldPath, Staged: change.StagedStatus != "", Unstaged: change.UnstagedStatus != "" || change.Untracked, ArtifactID: fileArtifacts[change.Path]}
		if err := p.record(ctx, safe, eventType, payload, &runtimeInstanceID, nil); err != nil {
			return err
		}
	}
	return nil
}

func candidateEventType(change evidence.CandidateChange) string {
	statuses := []string{change.StagedStatus, change.UnstagedStatus}
	if change.Untracked {
		return "file.created"
	}
	for _, status := range statuses {
		if status == "renamed" {
			return "file.renamed"
		}
	}
	for _, status := range statuses {
		if status == "deleted" {
			return "file.deleted"
		}
	}
	for _, status := range statuses {
		if status == "created" {
			return "file.created"
		}
	}
	return "file.modified"
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
		RuntimeInstanceID: runtimeInstanceID,
		ParentEventID:     parentEventID,
		Actor:             store.EmptyObject,
		Payload:           encoded,
	})
	return err
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

type processLauncher struct {
	sessions          SessionService
	events            *evidence.Recorder
	output            *evidence.OutputRecorder
	safe              executioncontext.SafeContext
	runtimeInstanceID string
	scope             evidence.RunScope
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
	process, err := l.sessions.Start(ctx, l.scope.ProjectID, l.scope.RunID, l.runtimeInstanceID, app.AuthorizedExecutionRequest{
		Command:               append([]string(nil), request.Command...),
		CWD:                   request.CWD,
		Env:                   cloneMap(request.Env),
		ProviderCredentialEnv: request.ProviderCredentialEnv,
		RuntimeSecretRefs:     cloneMap(request.RuntimeSecretRefs),
	})
	if err != nil {
		_ = l.recordFailure(ctx, request, &started.ID, nil, err)
		return nil, err
	}
	return newCapturingProcess(ctx, process, l, request, started.ID), nil
}

func (l *processLauncher) record(ctx context.Context, eventType string, payload any, parent *string) (store.Event, error) {
	encoded, err := evidence.EncodePayload(payload)
	if err != nil {
		return store.Event{}, err
	}
	issueID, runID, agentID, workspaceID, runtimeID := l.safe.Issue.ID, l.safe.Run.ID, l.safe.Agent.ID, l.safe.Workspace.ID, l.runtimeInstanceID
	return l.events.Record(ctx, store.Event{Type: eventType, ProjectID: l.safe.Project.ID, IssueID: &issueID, RunID: &runID, AgentID: &agentID, WorkspaceID: &workspaceID, RuntimeInstanceID: &runtimeID, ParentEventID: parent, Actor: store.EmptyObject, Payload: encoded})
}

func (l *processLauncher) recordFailure(ctx context.Context, request engine.ProcessRequest, parent *string, chunks []store.RawOutputChunk, cause error) error {
	payload := processPayload(request, nil, chunkIDs(chunks))
	eventType := "tool.failed"
	if request.Kind == "test" {
		eventType = "test.failed"
		payload = evidence.TestPayload{Command: append([]string(nil), request.Command...), Status: "failed", OutputChunkIDs: chunkIDs(chunks)}
	}
	_, err := l.record(ctx, eventType, map[string]any{"result": payload, "error": safeFailure(cause)}, parent)
	return err
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
	return p.process.Terminate(ctx)
}
func (p *capturingProcess) Kill(ctx context.Context) error { return p.process.Kill(ctx) }

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
		parent := p.parentEventID
		terminalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), processCancellationCleanupTimeout)
		defer cancel()
		if p.waitErr != nil || result.ExitCode != 0 {
			cause := p.waitErr
			if cause == nil {
				cause = fmt.Errorf("process exited with code %d", result.ExitCode)
			}
			if eventErr := p.launcher.recordFailure(terminalCtx, p.request, &parent, chunks, cause); eventErr != nil {
				p.waitErr = errors.Join(p.waitErr, eventErr)
			}
			return
		}
		eventType := "tool.completed"
		payload := processPayload(p.request, &exitCode, chunkIDs(chunks))
		if p.request.Kind == "test" {
			eventType = "test.completed"
		}
		if _, eventErr := p.launcher.record(terminalCtx, eventType, payload, &parent); eventErr != nil {
			p.waitErr = eventErr
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
