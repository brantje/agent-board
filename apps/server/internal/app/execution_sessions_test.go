package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

type executionSessionStoreFake struct {
	mu               sync.Mutex
	run              store.Run
	session          store.ExecutionSession
	sessionsByRunner []store.ExecutionSession
	getRunErr        error
	createErr        error
	transitionErr    error
	mutateTransition bool
}

func (s *executionSessionStoreFake) GetRun(context.Context, string, string) (store.Run, error) {
	if s.getRunErr != nil {
		return store.Run{}, s.getRunErr
	}
	return s.run, nil
}
func (s *executionSessionStoreFake) CreateExecutionSession(_ context.Context, input store.ExecutionSession) (store.ExecutionSession, error) {
	if s.createErr != nil {
		return store.ExecutionSession{}, s.createErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	input.ID = "session-1"
	s.session = input
	return input, nil
}
func (s *executionSessionStoreFake) GetExecutionSession(context.Context, string, string) (store.ExecutionSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session, nil
}
func (s *executionSessionStoreFake) ListExecutionSessionsByRunner(_ context.Context, runnerID string, statuses []string) ([]store.ExecutionSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidates := s.sessionsByRunner
	if len(candidates) == 0 && s.session.ID != "" {
		candidates = []store.ExecutionSession{s.session}
	}
	sessions := make([]store.ExecutionSession, 0, len(candidates))
	for _, session := range candidates {
		if runnerID == "" || session.RunnerID != runnerID {
			continue
		}
		if len(statuses) == 0 {
			sessions = append(sessions, session)
			continue
		}
		for _, status := range statuses {
			if session.Status == status {
				sessions = append(sessions, session)
				break
			}
		}
	}
	return sessions, nil
}
func (s *executionSessionStoreFake) TransitionExecutionSession(_ context.Context, tr store.ExecutionSessionTransition) (store.ExecutionSession, error) {
	if s.transitionErr != nil {
		return store.ExecutionSession{}, s.transitionErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := len(tr.FromStatuses) == 0
	for _, status := range tr.FromStatuses {
		if s.session.Status == status {
			allowed = true
			break
		}
	}
	if !allowed {
		return store.ExecutionSession{}, store.ErrConflict
	}
	s.session.Status = tr.Status
	s.session.ExitCode = tr.ExitCode
	if len(tr.CommandArgv) > 0 {
		s.session.CommandArgv = append(json.RawMessage(nil), tr.CommandArgv...)
	}
	updated := s.session
	if s.mutateTransition {
		updated.RunID = "mutated-run"
	}
	return updated, nil
}

type fakeExecutionTransport struct {
	id         string
	stdout     string
	stderr     string
	resultCh   chan struct{}
	result     runner.Result
	waitErr    error
	terminated bool
	killed     bool
}

func newFakeExecutionTransport(id string) *fakeExecutionTransport {
	return &fakeExecutionTransport{id: id, resultCh: make(chan struct{})}
}
func (t *fakeExecutionTransport) ID() string            { return t.id }
func (t *fakeExecutionTransport) Stdout() io.Reader     { return strings.NewReader(t.stdout) }
func (t *fakeExecutionTransport) Stderr() io.Reader     { return strings.NewReader(t.stderr) }
func (t *fakeExecutionTransport) Stdin() io.WriteCloser { return nopBuffer{Buffer: &bytes.Buffer{}} }
func (t *fakeExecutionTransport) Wait(ctx context.Context) (runner.Result, error) {
	select {
	case <-t.resultCh:
		return t.result, t.waitErr
	case <-ctx.Done():
		return runner.Result{}, ctx.Err()
	}
}
func (t *fakeExecutionTransport) Terminate(context.Context) error { t.terminated = true; return nil }
func (t *fakeExecutionTransport) Kill(context.Context) error      { t.killed = true; return nil }

type nopBuffer struct{ *bytes.Buffer }

func (nopBuffer) Close() error { return nil }

type fakeExecutionClient struct {
	transport runner.ProcessSession
	startErr  error
	done      chan struct{}
	request   runner.Request
}

func (c *fakeExecutionClient) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{MaxActiveSessions: 1}
}
func (c *fakeExecutionClient) Health() protocol.Health { return protocol.Health{Status: "ok"} }
func (c *fakeExecutionClient) Start(_ context.Context, _ string, request runner.Request) (runner.ProcessSession, error) {
	c.request = request
	return c.transport, c.startErr
}
func (c *fakeExecutionClient) Attach(string) (runner.ProcessSession, error) { return c.transport, nil }
func (c *fakeExecutionClient) Done() <-chan struct{}                        { return c.done }
func (c *fakeExecutionClient) Err() error                                   { return nil }
func (c *fakeExecutionClient) Close() error                                 { return nil }

type fakeExecutionManager struct {
	client       runner.Client
	err          error
	reconcile   runner.ProcessSession
	active      bool
	reconcileErr error
}

func (m *fakeExecutionManager) Connect(context.Context, string, string) (runner.Client, error) {
	return m.client, m.err
}
func (m *fakeExecutionManager) Reconcile(context.Context, string, string, string) (runner.ProcessSession, bool, error) {
	return m.reconcile, m.active, m.reconcileErr
}

func runnerOwnedExecutionService(t *testing.T) (*ExecutionSessionService, *executionSessionStoreFake, *fakeExecutionTransport, *fakeExecutionClient) {
	t.Helper()
	transport := newFakeExecutionTransport("session-1")
	client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
	registry := &fakeExecutionManager{client: client}
	storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"}}
	service, err := NewExecutionSessionService(storeFake, registry)
	if err != nil {
		t.Fatal(err)
	}
	return service, storeFake, transport, client
}

func TestExecutionSessionConstructorAndValidation(t *testing.T) {
	if _, err := NewExecutionSessionService(nil, nil); err == nil {
		t.Fatal("expected nil store rejection")
	}
	storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1", ProjectID: "project-1"}}
	service, err := NewExecutionSessionService(storeFake, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []ExecutionRequest{
		{},
		{Command: []string{""}},
		{Command: []string{"true"}, CWD: "/tmp"},
		{Command: []string{"true"}, CWD: "/workspace/../tmp"},
	} {
		if _, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", request); err == nil {
			t.Fatalf("request %+v unexpectedly succeeded", request)
		}
	}
	if _, err := service.StartOnRunner(context.Background(), "", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
		t.Fatal("blank project accepted")
	}
	if cloneMap(nil) != nil {
		t.Fatal("cloneMap(nil) must stay nil")
	}
	original := map[string]string{"A": "one"}
	cloned := cloneMap(original)
	cloned["A"] = "two"
	if original["A"] != "one" {
		t.Fatal("cloneMap aliased input")
	}
}

func TestStartOnRunnerPersistsCompletionAndRequest(t *testing.T) {
	service, storeFake, transport, client := runnerOwnedExecutionService(t)
	process, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", ExecutionRequest{
		Command: []string{"sh", "-c", "exit 5"},
		CWD:     "/workspace/sub",
		Env:     map[string]string{"A": "B"},
		Secrets: map[string]string{"TOKEN": "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if process.Record().Status != "RUNNING" || process.Record().RunnerID != "runner-1" {
		t.Fatalf("record=%+v", process.Record())
	}
	var argv []string
	if err := json.Unmarshal(storeFake.session.CommandArgv, &argv); err != nil || len(argv) != 3 {
		t.Fatalf("argv=%v err=%v", argv, err)
	}
	if client.request.Dir != "/workspace/sub" || client.request.Env["A"] != "B" || client.request.Secrets["TOKEN"] != "secret" {
		t.Fatalf("request=%+v", client.request)
	}
	transport.result = runner.Result{ExitCode: 5}
	close(transport.resultCh)
	result, err := process.Wait(context.Background())
	if err != nil || result.ExitCode != 5 || process.Record().Status != "COMPLETED" {
		t.Fatalf("result=%+v record=%+v err=%v", result, process.Record(), err)
	}
}

func TestStartOnRunnerFailureBranches(t *testing.T) {
	t.Run("run lookup", func(t *testing.T) {
		storeFake := &executionSessionStoreFake{getRunErr: store.ErrNotFound}
		service, _ := NewExecutionSessionService(storeFake, &fakeExecutionManager{})
		if _, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
			t.Fatal("expected run lookup error")
		}
	})

	t.Run("create conflict", func(t *testing.T) {
		storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1"}, createErr: store.ErrConflict}
		service, _ := NewExecutionSessionService(storeFake, &fakeExecutionManager{})
		if _, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
			t.Fatal("expected create conflict")
		}
	})

	t.Run("immutable binding", func(t *testing.T) {
		transport := newFakeExecutionTransport("session-1")
		client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
		storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1"}, mutateTransition: true}
		service, _ := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client})
		if _, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
			t.Fatal("expected immutable binding error")
		}
	})

	t.Run("connect failure persists failed", func(t *testing.T) {
		storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1"}}
		service, _ := NewExecutionSessionService(storeFake, &fakeExecutionManager{err: errors.New("dial failed")})
		if _, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}}); err == nil || storeFake.session.Status != "FAILED" {
			t.Fatalf("session=%+v err=%v", storeFake.session, err)
		}
	})

	t.Run("protocol start failure persists failed", func(t *testing.T) {
		transport := newFakeExecutionTransport("session-1")
		client := &fakeExecutionClient{transport: transport, startErr: &runner.ProtocolError{Code: "rejected", Message: "no"}, done: make(chan struct{})}
		storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1"}}
		service, _ := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client})
		if _, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}}); err == nil || storeFake.session.Status != "FAILED" {
			t.Fatalf("session=%+v err=%v", storeFake.session, err)
		}
	})

	t.Run("transport start failure stays uncertain", func(t *testing.T) {
		transport := newFakeExecutionTransport("session-1")
		client := &fakeExecutionClient{transport: transport, startErr: runner.ErrDisconnected, done: make(chan struct{})}
		storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1"}}
		service, _ := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client})
		if _, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}}); err == nil || storeFake.session.Status != "STARTING" {
			t.Fatalf("session=%+v err=%v", storeFake.session, err)
		}
	})
}

func TestExecutionSessionDisconnectRemainsNonTerminal(t *testing.T) {
	service, _, transport, _ := runnerOwnedExecutionService(t)
	process, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"sleep", "10"}})
	if err != nil {
		t.Fatal(err)
	}
	transport.waitErr = runner.ErrDisconnected
	close(transport.resultCh)
	_, err = process.Wait(context.Background())
	if !errors.Is(err, runner.ErrDisconnected) || process.Record().Status != "RUNNING" {
		t.Fatalf("record=%+v err=%v", process.Record(), err)
	}
}

func TestExecutionSessionCancellationTargetsSession(t *testing.T) {
	service, _, transport, _ := runnerOwnedExecutionService(t)
	process, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"sleep", "10"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Terminate(context.Background()); err != nil || !transport.terminated {
		t.Fatalf("Terminate() err=%v called=%v", err, transport.terminated)
	}
	transport.result = runner.Result{ExitCode: 143, Signaled: true}
	close(transport.resultCh)
	_, err = process.Wait(context.Background())
	if err != nil || process.Record().Status != "CANCELLED" {
		t.Fatalf("record=%+v err=%v", process.Record(), err)
	}
}

func TestCreateRunnerSessionPersistsRunnerOwnership(t *testing.T) {
	service, storeFake, _, _ := runnerOwnedExecutionService(t)
	session, err := service.CreateRunnerSession(context.Background(), "project-1", "run-1", "runner-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.RunnerID != "runner-1" || session.Status != "PENDING" || storeFake.session.RunnerID != "runner-1" {
		t.Fatalf("session=%+v persisted=%+v", session, storeFake.session)
	}
	if _, err := service.CreateRunnerSession(context.Background(), "", "run-1", "runner-1"); err == nil {
		t.Fatal("blank ids accepted")
	}
}

func TestStartPreparedOnRunnerUsesSelectedRunner(t *testing.T) {
	service, _, transport, _ := runnerOwnedExecutionService(t)
	session, err := service.CreateRunnerSession(context.Background(), "project-1", "run-1", "runner-1")
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.StartPreparedOnRunner(context.Background(), "project-1", session.ID, ExecutionRequest{Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	if process.Record().Status != "RUNNING" || process.Record().RunnerID != "runner-1" {
		t.Fatalf("record=%+v", process.Record())
	}
	if !strings.Contains(string(process.Record().CommandArgv), "true") {
		t.Fatalf("command argv=%s", process.Record().CommandArgv)
	}
	transport.result = runner.Result{ExitCode: 0}
	close(transport.resultCh)
	if _, err := process.Wait(context.Background()); err != nil || process.Record().Status != "COMPLETED" {
		t.Fatalf("record=%+v err=%v", process.Record(), err)
	}
}

func TestStartPreparedOnRunnerRejectsSessionWithoutRunner(t *testing.T) {
	service, storeFake, _, _ := runnerOwnedExecutionService(t)
	storeFake.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", Status: "PENDING"}
	if _, err := service.StartPreparedOnRunner(context.Background(), "project-1", "session-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
		t.Fatal("session without Runner accepted")
	}
}

func TestStartPreparedOnRunnerRejectsRunningSession(t *testing.T) {
	service, _, _, _ := runnerOwnedExecutionService(t)
	session, err := service.CreateRunnerSession(context.Background(), "project-1", "run-1", "runner-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartPreparedOnRunner(context.Background(), "project-1", session.ID, ExecutionRequest{Command: []string{"true"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartPreparedOnRunner(context.Background(), "project-1", session.ID, ExecutionRequest{Command: []string{"true"}}); err == nil {
		t.Fatal("duplicate start on running session accepted")
	}
}
