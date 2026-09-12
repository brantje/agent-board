package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const workspaceTarget = "/workspace"

type ExecutionSessionStore interface {
	GetRun(context.Context, string, string) (store.Run, error)
	CreateExecutionSession(context.Context, store.ExecutionSession) (store.ExecutionSession, error)
	GetExecutionSession(context.Context, string, string) (store.ExecutionSession, error)
	ListExecutionSessionsByRunner(context.Context, string, []string) ([]store.ExecutionSession, error)
	TransitionExecutionSession(context.Context, store.ExecutionSessionTransition) (store.ExecutionSession, error)
}

type RunnerRegistry interface {
	Connect(context.Context, string, string) (runner.Client, error)
	Reconcile(context.Context, string, string, string) (runner.ProcessSession, bool, error)
}

type noopRunnerRegistry struct{}

func (noopRunnerRegistry) Connect(context.Context, string, string) (runner.Client, error) {
	return nil, errors.New("runner registry is not configured")
}

func (noopRunnerRegistry) Reconcile(context.Context, string, string, string) (runner.ProcessSession, bool, error) {
	return nil, false, errors.New("runner registry is not configured")
}

type ExecutionRequest struct {
	Command []string
	CWD     string
	Env     map[string]string
	Secrets map[string]string
}

type ExecutionSessionService struct {
	store    ExecutionSessionStore
	registry RunnerRegistry

	reconnectTimeoutNanos atomic.Int64
	liveMu                sync.RWMutex
	live                  map[string]*ExecutionProcess
}

func NewExecutionSessionService(sessionStore ExecutionSessionStore, registry RunnerRegistry) (*ExecutionSessionService, error) {
	if sessionStore == nil {
		return nil, fmt.Errorf("execution session store is required")
	}
	if registry == nil {
		registry = noopRunnerRegistry{}
	}
	return &ExecutionSessionService{store: sessionStore, registry: registry, live: make(map[string]*ExecutionProcess)}, nil
}

func (s *ExecutionSessionService) CreateRunnerSession(ctx context.Context, projectID, runID, runnerID string) (store.ExecutionSession, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(runID) == "" || strings.TrimSpace(runnerID) == "" {
		return store.ExecutionSession{}, NewError("invalid_argument", "projectId, runId and runnerId are required", store.ErrInvalidArgument)
	}
	if _, err := s.store.GetRun(ctx, projectID, runID); err != nil {
		return store.ExecutionSession{}, translateStoreError(err, "run")
	}
	session, err := s.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: projectID, RunID: runID, RunnerID: runnerID,
		Status: "PENDING", CWD: workspaceTarget, CommandArgv: []byte("[]"),
	})
	if err != nil {
		return store.ExecutionSession{}, translateStoreError(err, "execution_session")
	}
	return session, nil
}

func (s *ExecutionSessionService) StartPreparedOnRunner(ctx context.Context, projectID, sessionID string, request ExecutionRequest) (*ExecutionProcess, error) {
	session, err := s.store.GetExecutionSession(ctx, projectID, sessionID)
	if err != nil {
		return nil, translateStoreError(err, "execution_session")
	}
	if session.RunnerID == "" {
		return nil, NewError("invalid_argument", "execution session is not runner-owned", store.ErrInvalidArgument)
	}
	cwd, err := validateRunnerExecutionRequest(projectID, session.RunID, session.RunnerID, request)
	if err != nil {
		return nil, err
	}
	fromStatus := []string{"PENDING"}
	switch session.Status {
	case "PENDING":
	case "COMPLETED":
		fromStatus = []string{"COMPLETED"}
	default:
		return nil, NewError("execution_session_invalid_state", "Execution Session is not pending workspace transfer", store.ErrInvalidArgument)
	}
	argv, err := json.Marshal(request.Command)
	if err != nil {
		return nil, fmt.Errorf("encode execution command: %w", err)
	}
	return s.startRunnerSession(ctx, projectID, session, fromStatus, cwd, argv, request)
}

func (s *ExecutionSessionService) StartOnRunner(ctx context.Context, projectID, runID, runnerID string, request ExecutionRequest) (*ExecutionProcess, error) {
	cwd, err := validateRunnerExecutionRequest(projectID, runID, runnerID, request)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetRun(ctx, projectID, runID); err != nil {
		return nil, translateStoreError(err, "run")
	}
	argv, err := json.Marshal(request.Command)
	if err != nil {
		return nil, fmt.Errorf("encode execution command: %w", err)
	}
	session, err := s.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: projectID, RunID: runID, RunnerID: runnerID,
		Status: "PENDING", CWD: cwd, CommandArgv: argv,
	})
	if err != nil {
		return nil, translateStoreError(err, "execution_session")
	}
	return s.startRunnerSession(ctx, projectID, session, []string{"PENDING"}, cwd, argv, request)
}

func (s *ExecutionSessionService) startRunnerSession(ctx context.Context, projectID string, session store.ExecutionSession, fromStatus []string, cwd string, argv []byte, request ExecutionRequest) (*ExecutionProcess, error) {
	previous := session
	started, err := s.store.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: session.ProjectID, SessionID: session.ID, FromStatuses: fromStatus, Status: "STARTING", CommandArgv: argv,
	})
	if err != nil {
		return nil, translateStoreError(err, "execution_session")
	}
	if started.RunID != previous.RunID || started.RunnerID != previous.RunnerID {
		return nil, fmt.Errorf("execution session immutable binding changed during transition")
	}

	client, err := s.registry.Connect(ctx, projectID, started.RunnerID)
	if err != nil {
		_, failErr := s.transition(ctx, started, []string{"STARTING"}, "FAILED", nil)
		return nil, errors.Join(fmt.Errorf("connect runner: %w", err), failErr)
	}
	transport, err := client.Start(ctx, started.ID, runner.Request{
		Command: append([]string(nil), request.Command...),
		Dir:     cwd,
		Env:     cloneMap(request.Env),
		Secrets: cloneMap(request.Secrets),
	})
	if err != nil {
		var protocolErr *runner.ProtocolError
		if errors.As(err, &protocolErr) {
			_, failErr := s.transition(ctx, started, []string{"STARTING"}, "FAILED", nil)
			return nil, errors.Join(err, failErr)
		}
		if transport != nil {
			s.retainExecutionProcess(started, transport)
		}
		return nil, NewError("execution_session_uncertain", "runner transport was interrupted while starting the Execution Session; reconciliation is required", err)
	}
	runningSession, transitionErr := s.transition(ctx, started, []string{"STARTING"}, "RUNNING", nil)
	if transitionErr != nil {
		s.retainExecutionProcess(started, transport)
		return nil, NewError("execution_session_uncertain", "Execution Session started but durable RUNNING state could not be confirmed", transitionErr)
	}
	return newExecutionProcess(s, runningSession, transport), nil
}

func (s *ExecutionSessionService) retainExecutionProcess(session store.ExecutionSession, transport runner.ProcessSession) *ExecutionProcess {
	process := newExecutionProcess(s, session, transport)
	go func() { _, _ = io.Copy(io.Discard, process.Stdout()) }()
	go func() { _, _ = io.Copy(io.Discard, process.Stderr()) }()
	return process
}

func validateRunnerExecutionRequest(projectID, runID, runnerID string, request ExecutionRequest) (string, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(runID) == "" || strings.TrimSpace(runnerID) == "" {
		return "", NewError("invalid_argument", "projectId, runId and runnerId are required", store.ErrInvalidArgument)
	}
	return validateExecutionCWD(request)
}

func validateExecutionCWD(request ExecutionRequest) (string, error) {
	if len(request.Command) == 0 || strings.TrimSpace(request.Command[0]) == "" {
		return "", NewError("invalid_argument", "execution command is required", store.ErrInvalidArgument)
	}
	cwd := request.CWD
	if cwd == "" {
		cwd = workspaceTarget
	}
	clean := path.Clean(cwd)
	if clean != workspaceTarget && !strings.HasPrefix(clean, workspaceTarget+"/") {
		return "", NewError("invalid_argument", "execution cwd must stay within /workspace", store.ErrInvalidArgument)
	}
	return clean, nil
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

func (s *ExecutionSessionService) transition(ctx context.Context, session store.ExecutionSession, from []string, status string, exitCode *int) (store.ExecutionSession, error) {
	updated, err := s.store.TransitionExecutionSession(ctx, store.ExecutionSessionTransition{
		ProjectID: session.ProjectID, SessionID: session.ID, FromStatuses: from, Status: status, ExitCode: exitCode,
	})
	if err != nil {
		return store.ExecutionSession{}, translateStoreError(err, "execution_session")
	}
	if updated.RunID != session.RunID || updated.RunnerID != session.RunnerID {
		return store.ExecutionSession{}, fmt.Errorf("execution session immutable binding changed during transition")
	}
	return updated, nil
}

func liveProcessKey(projectID, sessionID string) string {
	return projectID + "/" + sessionID
}

func (s *ExecutionSessionService) trackProcess(process *ExecutionProcess) {
	record := process.Record()
	s.liveMu.Lock()
	s.live[liveProcessKey(record.ProjectID, record.ID)] = process
	s.liveMu.Unlock()
}

func (s *ExecutionSessionService) untrackProcess(process *ExecutionProcess) {
	record := process.Record()
	key := liveProcessKey(record.ProjectID, record.ID)
	s.liveMu.Lock()
	if s.live[key] == process {
		delete(s.live, key)
	}
	s.liveMu.Unlock()
}

func (s *ExecutionSessionService) liveProcess(projectID, sessionID string) (*ExecutionProcess, bool) {
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()
	process, ok := s.live[liveProcessKey(projectID, sessionID)]
	return process, ok
}

type ExecutionProcess struct {
	service   *ExecutionSessionService
	transport runner.ProcessSession

	mu       sync.RWMutex
	cancelMu sync.Mutex
	record   store.ExecutionSession
	result   runner.Result
	err      error
	done     chan struct{}
	cancel   atomic.Bool
}

func newExecutionProcess(service *ExecutionSessionService, record store.ExecutionSession, transport runner.ProcessSession) *ExecutionProcess {
	process := &ExecutionProcess{service: service, transport: transport, record: record, done: make(chan struct{})}
	service.trackProcess(process)
	go process.observe()
	return process
}

func (p *ExecutionProcess) ID() string            { return p.transport.ID() }
func (p *ExecutionProcess) Stdout() io.Reader     { return p.transport.Stdout() }
func (p *ExecutionProcess) Stderr() io.Reader     { return p.transport.Stderr() }
func (p *ExecutionProcess) Stdin() io.WriteCloser { return p.transport.Stdin() }
func (p *ExecutionProcess) AbandonStdout() error  { return runner.AbandonStdout(p.transport) }
func (p *ExecutionProcess) AbandonStderr() error  { return runner.AbandonStderr(p.transport) }

func (p *ExecutionProcess) Record() store.ExecutionSession {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.record
}

func (p *ExecutionProcess) Wait(ctx context.Context) (runner.Result, error) {
	select {
	case <-p.done:
		p.mu.RLock()
		defer p.mu.RUnlock()
		return p.result, p.err
	case <-ctx.Done():
		return runner.Result{}, ctx.Err()
	}
}

func (p *ExecutionProcess) Terminate(ctx context.Context) error {
	return p.signalCancellation(ctx, p.transport.Terminate)
}

func (p *ExecutionProcess) Kill(ctx context.Context) error {
	return p.signalCancellation(ctx, p.transport.Kill)
}

func (p *ExecutionProcess) signalCancellation(ctx context.Context, signal func(context.Context) error) error {
	p.cancelMu.Lock()
	defer p.cancelMu.Unlock()

	alreadyRequested := p.cancel.Load()
	p.cancel.Store(true)
	if err := signal(ctx); err != nil {
		if !alreadyRequested {
			select {
			case <-p.done:
			default:
				p.cancel.Store(false)
			}
		}
		return err
	}
	return nil
}

func (p *ExecutionProcess) observe() {
	defer p.service.untrackProcess(p)
	result, waitErr := p.transport.Wait(context.Background())
	record := p.Record()
	var finalErr error
	if waitErr != nil {
		if errors.Is(waitErr, runner.ErrDisconnected) || errors.Is(waitErr, runner.ErrClosed) {
			finalErr = waitErr
		} else {
			updated, transitionErr := p.service.transition(context.Background(), record, []string{"STARTING", "RUNNING"}, "FAILED", nil)
			if transitionErr == nil {
				record = updated
			}
			finalErr = errors.Join(waitErr, transitionErr)
		}
	} else {
		status := "COMPLETED"
		if p.cancel.Load() {
			status = "CANCELLED"
		}
		exitCode := result.ExitCode
		updated, transitionErr := p.service.transition(context.Background(), record, []string{"STARTING", "RUNNING"}, status, &exitCode)
		if transitionErr == nil {
			record = updated
		}
		finalErr = transitionErr
	}
	p.mu.Lock()
	p.record = record
	p.result = result
	p.err = finalErr
	p.mu.Unlock()
	close(p.done)
}
