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
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const executionRecoveryTimeout = 5 * time.Second

type ExecutionSessionStore interface {
	GetRun(context.Context, string, string) (store.Run, error)
	GetRuntimeInstance(context.Context, string, string) (store.RuntimeInstance, error)
	CreateExecutionSession(context.Context, store.ExecutionSession) (store.ExecutionSession, error)
	GetExecutionSession(context.Context, string, string) (store.ExecutionSession, error)
	TransitionExecutionSession(context.Context, store.ExecutionSessionTransition) (store.ExecutionSession, error)
	UpdateRuntimeInstanceRunnerStatus(context.Context, string, string, string) (store.RuntimeInstance, error)
}

type RunnerConnectionManager interface {
	Connect(context.Context, string, string) (runner.Client, error)
	Reconcile(context.Context, string, string, string) (runner.ProcessSession, bool, error)
}

type ExecutionRequest struct {
	Command []string
	CWD     string
	Env     map[string]string
	Secrets map[string]string
}

type ExecutionSessionService struct {
	store   ExecutionSessionStore
	runners RunnerConnectionManager

	liveMu sync.RWMutex
	live   map[string]*ExecutionProcess
}

func NewExecutionSessionService(sessionStore ExecutionSessionStore, runners RunnerConnectionManager) (*ExecutionSessionService, error) {
	if sessionStore == nil || runners == nil {
		return nil, fmt.Errorf("execution session store and runner manager are required")
	}
	return &ExecutionSessionService{store: sessionStore, runners: runners, live: make(map[string]*ExecutionProcess)}, nil
}

func (s *ExecutionSessionService) Start(ctx context.Context, projectID, runID, runtimeInstanceID string, request ExecutionRequest) (*ExecutionProcess, error) {
	cwd, err := validateExecutionRequest(projectID, runID, runtimeInstanceID, request)
	if err != nil {
		return nil, err
	}
	run, err := s.store.GetRun(ctx, projectID, runID)
	if err != nil {
		return nil, translateStoreError(err, "run")
	}
	instance, err := s.store.GetRuntimeInstance(ctx, projectID, runtimeInstanceID)
	if err != nil {
		return nil, translateStoreError(err, "runtime_instance")
	}
	if run.WorkspaceID != instance.WorkspaceID {
		return nil, NewError("runtime_workspace_mismatch", "Run and Runtime Instance must use the same Workspace", store.ErrInvalidArgument)
	}
	if instance.Status != string(runtimepkg.StateRunning) {
		return nil, NewError("runtime_instance_not_running", "Runtime Instance is not running", runtimepkg.ErrRunnerUnavailable)
	}
	argv, err := json.Marshal(request.Command)
	if err != nil {
		return nil, fmt.Errorf("encode execution command: %w", err)
	}
	session, err := s.store.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: projectID, RunID: runID, RuntimeInstanceID: runtimeInstanceID,
		Status: "PENDING", CWD: cwd, CommandArgv: argv,
	})
	if err != nil {
		return nil, translateStoreError(err, "execution_session")
	}
	session, err = s.transition(ctx, session, []string{"PENDING"}, "STARTING", nil)
	if err != nil {
		return nil, err
	}

	client, err := s.runners.Connect(ctx, projectID, runtimeInstanceID)
	if err != nil {
		_, failErr := s.transition(ctx, session, []string{"STARTING"}, "FAILED", nil)
		return nil, errors.Join(fmt.Errorf("connect runner: %w", err), failErr)
	}
	transport, err := client.Start(ctx, session.ID, runner.Request{Command: append([]string(nil), request.Command...), Dir: cwd, Env: cloneMap(request.Env), Secrets: cloneMap(request.Secrets)})
	if err != nil {
		var protocolErr *runner.ProtocolError
		if errors.As(err, &protocolErr) {
			_, failErr := s.transition(ctx, session, []string{"STARTING"}, "FAILED", nil)
			return nil, errors.Join(err, failErr)
		}
		uncertainCause := err
		if transport != nil {
			// A start request may have reached the runner before the caller's
			// context expired. Mark the runner BUSY with an independent recovery
			// context before exposing the retained process, then keep exactly one
			// observer attached so output cannot backpressure the connection and a
			// later terminal result still updates durable state.
			if statusErr := s.updateRunnerStatusRecovery(projectID, runtimeInstanceID, "BUSY"); statusErr != nil {
				uncertainCause = errors.Join(uncertainCause, fmt.Errorf("persist runner BUSY status: %w", statusErr))
			}
			s.retainExecutionProcess(session, transport)
		}
		return nil, NewError("execution_session_uncertain", "runner transport was interrupted while starting the Execution Session; reconciliation is required", uncertainCause)
	}

	runningSession, err := s.transition(ctx, session, []string{"STARTING"}, "RUNNING", nil)
	if err != nil {
		s.retainExecutionProcess(session, transport)
		return nil, NewError("execution_session_uncertain", "Execution Session started but durable RUNNING state could not be confirmed", err)
	}
	session = runningSession
	if _, err := s.store.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, runtimeInstanceID, "BUSY"); err != nil {
		s.retainExecutionProcess(session, transport)
		return nil, NewError("execution_session_uncertain", "Execution Session started but runner BUSY state could not be persisted", err)
	}
	return newExecutionProcess(s, session, transport), nil
}

func (s *ExecutionSessionService) retainExecutionProcess(session store.ExecutionSession, transport runner.ProcessSession) *ExecutionProcess {
	process := newExecutionProcess(s, session, transport)
	go func() { _, _ = io.Copy(io.Discard, process.Stdout()) }()
	go func() { _, _ = io.Copy(io.Discard, process.Stderr()) }()
	return process
}

func (s *ExecutionSessionService) updateRunnerStatusRecovery(projectID, runtimeInstanceID, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), executionRecoveryTimeout)
	defer cancel()
	_, err := s.store.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, runtimeInstanceID, status)
	return err
}

func validateExecutionRequest(projectID, runID, runtimeInstanceID string, request ExecutionRequest) (string, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(runID) == "" || strings.TrimSpace(runtimeInstanceID) == "" {
		return "", NewError("invalid_argument", "projectId, runId and runtimeInstanceId are required", store.ErrInvalidArgument)
	}
	if len(request.Command) == 0 || strings.TrimSpace(request.Command[0]) == "" {
		return "", NewError("invalid_argument", "execution command is required", store.ErrInvalidArgument)
	}
	cwd := request.CWD
	if cwd == "" {
		cwd = runtimepkg.WorkspaceTarget
	}
	clean := path.Clean(cwd)
	if clean != runtimepkg.WorkspaceTarget && !strings.HasPrefix(clean, runtimepkg.WorkspaceTarget+"/") {
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
	if updated.RunID != session.RunID || updated.RuntimeInstanceID != session.RuntimeInstanceID {
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

	mu     sync.RWMutex
	record store.ExecutionSession
	result runner.Result
	err    error
	done   chan struct{}
	cancel atomic.Bool
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
	p.cancel.Store(true)
	if err := p.transport.Terminate(ctx); err != nil {
		select {
		case <-p.done:
		default:
			p.cancel.Store(false)
		}
		return err
	}
	return nil
}

func (p *ExecutionProcess) Kill(ctx context.Context) error {
	p.cancel.Store(true)
	if err := p.transport.Kill(ctx); err != nil {
		select {
		case <-p.done:
		default:
			p.cancel.Store(false)
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
		if errors.Is(waitErr, runner.ErrDisconnected) || errors.Is(waitErr, runner.ErrClosed) || errors.Is(waitErr, runner.ErrManagerClosed) {
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
	if record.Status == "COMPLETED" || record.Status == "FAILED" || record.Status == "CANCELLED" {
		if _, statusErr := p.service.store.UpdateRuntimeInstanceRunnerStatus(context.Background(), record.ProjectID, record.RuntimeInstanceID, "READY"); statusErr != nil {
			finalErr = errors.Join(finalErr, statusErr)
		}
	}
	p.mu.Lock()
	p.record = record
	p.result = result
	p.err = finalErr
	p.mu.Unlock()
	close(p.done)
}
