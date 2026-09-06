package app

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
	sharedredact "github.com/brantje/agent-board/packages/redact"
)

type ExecutionPreparer interface {
	Prepare(context.Context, string, string, executioncontext.SecretRequest) (executioncontext.Prepared, error)
}

type AuthorizedExecutionRequest struct {
	Command               []string
	CWD                   string
	Env                   map[string]string
	ProviderCredentialEnv string
	RuntimeSecretRefs     map[string]string
}

// AuthorizedExecutionSessionService is the server-owned launch boundary. It
// resolves immutable execution context and authorized secret references before
// delegating the already-scoped request to the runner transport service.
type AuthorizedExecutionSessionService struct {
	sessions *ExecutionSessionService
	preparer ExecutionPreparer
}

func NewAuthorizedExecutionSessionService(sessions *ExecutionSessionService, preparer ExecutionPreparer) (*AuthorizedExecutionSessionService, error) {
	if sessions == nil || preparer == nil {
		return nil, NewError("execution_preparation_unavailable", "Execution preparation is unavailable", store.ErrInvalidArgument)
	}
	return &AuthorizedExecutionSessionService{sessions: sessions, preparer: preparer}, nil
}

func (s *AuthorizedExecutionSessionService) Start(ctx context.Context, projectID, runID, runtimeInstanceID string, request AuthorizedExecutionRequest) (*AuthorizedExecutionProcess, error) {
	prepared, err := s.preparer.Prepare(ctx, projectID, runID, executioncontext.SecretRequest{
		ProviderCredentialEnv: request.ProviderCredentialEnv,
		RuntimeSecretRefs:     cloneMap(request.RuntimeSecretRefs),
	})
	if err != nil {
		return nil, translateExecutionPreparationError(err)
	}
	releaseRedaction := prepared.ReleaseRedaction
	releaseTransferred := false
	defer func() {
		if !releaseTransferred && releaseRedaction != nil {
			releaseRedaction()
		}
	}()

	instance, err := s.sessions.store.GetRuntimeInstance(ctx, projectID, runtimeInstanceID)
	if err != nil {
		return nil, translateStoreError(err, "runtime_instance")
	}
	if instance.RuntimeID != prepared.RuntimeID {
		return nil, NewError("runtime_configuration_mismatch", "Runtime Instance does not match the resolved execution Runtime", store.ErrInvalidArgument)
	}
	process, err := s.sessions.Start(ctx, projectID, runID, runtimeInstanceID, ExecutionRequest{
		Command: append([]string(nil), request.Command...),
		CWD:     request.CWD,
		Env:     cloneMap(request.Env),
		Secrets: cloneMap(prepared.Secrets),
	})
	if err != nil {
		return nil, redaction.WrapError(err, prepared.RedactionValues)
	}
	authorized := newAuthorizedExecutionProcess(process, prepared.RedactionValues, releaseRedaction)
	releaseTransferred = true
	return authorized, nil
}

// AuthorizedExecutionProcess exposes only lifecycle operations that preserve
// the trusted redaction boundary. The raw ExecutionProcess is intentionally
// private so callers cannot bypass sanitized stdout/stderr readers.
type AuthorizedExecutionProcess struct {
	process         *ExecutionProcess
	stdout          *completionReader
	stderr          *completionReader
	redactionValues []string

	lifecycleMu sync.Mutex
	terminal    bool
	stdoutDone  bool
	stderrDone  bool
	release     func()
}

type completionReader struct {
	mu       sync.Mutex
	source   io.Reader
	buffer   *bytes.Reader
	settled  bool
	finalErr error
	onDone   func()
	once     sync.Once
}

func (r *completionReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.buffer != nil {
		n, err := r.buffer.Read(p)
		if err == io.EOF {
			r.buffer = nil
			if r.finalErr != nil {
				err = r.finalErr
			}
		}
		r.mu.Unlock()
		return n, err
	}
	if r.settled {
		err := r.finalErr
		if err == nil {
			err = io.EOF
		}
		r.mu.Unlock()
		return 0, err
	}
	n, err := r.source.Read(p)
	if err != nil {
		r.settled = true
		if err != io.EOF {
			r.finalErr = err
		}
	}
	r.mu.Unlock()
	if err != nil {
		r.complete()
	}
	return n, err
}

func (r *completionReader) settle() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.settled {
		r.mu.Unlock()
		r.complete()
		return
	}
	data, err := io.ReadAll(r.source)
	r.settled = true
	r.finalErr = err
	if len(data) > 0 {
		r.buffer = bytes.NewReader(data)
	}
	r.mu.Unlock()
	r.complete()
}

func (r *completionReader) abandon() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.buffer = nil
	r.settled = true
	r.finalErr = nil
	r.mu.Unlock()
	r.complete()
}

func (r *completionReader) complete() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.onDone != nil {
			r.onDone()
		}
	})
}

func newAuthorizedExecutionProcess(process *ExecutionProcess, redactionValues []string, releases ...func()) *AuthorizedExecutionProcess {
	values := append([]string(nil), redactionValues...)
	var release func()
	if len(releases) > 0 {
		release = releases[0]
	}
	authorized := &AuthorizedExecutionProcess{
		process:         process,
		redactionValues: values,
		release:         release,
	}
	authorized.stdout = &completionReader{source: sharedredact.NewReader(process.Stdout(), values), onDone: authorized.markStdoutDone}
	authorized.stderr = &completionReader{source: sharedredact.NewReader(process.Stderr(), values), onDone: authorized.markStderrDone}
	go func() {
		_, _ = process.Wait(context.Background())
		authorized.settleTerminalOutput()
	}()
	return authorized
}

func (p *AuthorizedExecutionProcess) ID() string        { return p.process.ID() }
func (p *AuthorizedExecutionProcess) Stdout() io.Reader { return p.stdout }
func (p *AuthorizedExecutionProcess) Stderr() io.Reader { return p.stderr }
func (p *AuthorizedExecutionProcess) Stdin() io.WriteCloser {
	return p.process.Stdin()
}
func (p *AuthorizedExecutionProcess) AbandonStdout() error {
	err := p.process.AbandonStdout()
	if err == nil {
		p.stdout.abandon()
	}
	return redaction.WrapError(err, p.redactionValues)
}
func (p *AuthorizedExecutionProcess) AbandonStderr() error {
	err := p.process.AbandonStderr()
	if err == nil {
		p.stderr.abandon()
	}
	return redaction.WrapError(err, p.redactionValues)
}
func (p *AuthorizedExecutionProcess) Record() store.ExecutionSession {
	return p.process.Record()
}
func (p *AuthorizedExecutionProcess) Wait(ctx context.Context) (runner.Result, error) {
	result, err := p.process.Wait(ctx)
	return result, redaction.WrapError(err, p.redactionValues)
}
func (p *AuthorizedExecutionProcess) Terminate(ctx context.Context) error {
	return redaction.WrapError(p.process.Terminate(ctx), p.redactionValues)
}
func (p *AuthorizedExecutionProcess) Kill(ctx context.Context) error {
	return redaction.WrapError(p.process.Kill(ctx), p.redactionValues)
}

func (p *AuthorizedExecutionProcess) settleTerminalOutput() {
	p.markTerminal()
	p.stdout.settle()
	p.stderr.settle()
}

func (p *AuthorizedExecutionProcess) markTerminal() {
	p.lifecycleMu.Lock()
	p.terminal = true
	release := p.takeReleaseLocked()
	p.lifecycleMu.Unlock()
	if release != nil {
		release()
	}
}

func (p *AuthorizedExecutionProcess) markStdoutDone() {
	p.lifecycleMu.Lock()
	p.stdoutDone = true
	release := p.takeReleaseLocked()
	p.lifecycleMu.Unlock()
	if release != nil {
		release()
	}
}

func (p *AuthorizedExecutionProcess) markStderrDone() {
	p.lifecycleMu.Lock()
	p.stderrDone = true
	release := p.takeReleaseLocked()
	p.lifecycleMu.Unlock()
	if release != nil {
		release()
	}
}

func (p *AuthorizedExecutionProcess) takeReleaseLocked() func() {
	if !p.terminal || !p.stdoutDone || !p.stderrDone || p.release == nil {
		return nil
	}
	release := p.release
	p.release = nil
	return release
}

func (s *AuthorizedExecutionSessionService) ReconcileAll(ctx context.Context) error {
	return s.sessions.ReconcileAll(ctx)
}

func (s *AuthorizedExecutionSessionService) ReconcileAllWithReporter(ctx context.Context, report func(error)) error {
	return s.sessions.ReconcileAllWithReporter(ctx, report)
}

func (s *AuthorizedExecutionSessionService) Cancel(ctx context.Context, projectID, sessionID string, grace time.Duration) error {
	return s.sessions.Cancel(ctx, projectID, sessionID, grace)
}

func translateExecutionPreparationError(err error) error {
	if executionErr, ok := executioncontext.AsError(err); ok {
		return NewError(executionErr.Code, executionErr.Message, executionErr.Cause)
	}
	return err
}
