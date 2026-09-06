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
	mu       sync.Mutex
	run      store.Run
	instance store.RuntimeInstance
	session  store.ExecutionSession
}
func (s *executionSessionStoreFake) GetRun(context.Context, string, string) (store.Run, error) { return s.run, nil }
func (s *executionSessionStoreFake) GetRuntimeInstance(context.Context, string, string) (store.RuntimeInstance, error) { return s.instance, nil }
func (s *executionSessionStoreFake) CreateExecutionSession(_ context.Context, input store.ExecutionSession) (store.ExecutionSession, error) {
	s.mu.Lock(); defer s.mu.Unlock(); input.ID = "session-1"; s.session = input; return input, nil
}
func (s *executionSessionStoreFake) GetExecutionSession(context.Context, string, string) (store.ExecutionSession, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.session, nil }
func (s *executionSessionStoreFake) TransitionExecutionSession(_ context.Context, tr store.ExecutionSessionTransition) (store.ExecutionSession, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	allowed := len(tr.FromStatuses) == 0
	for _, status := range tr.FromStatuses { if s.session.Status == status { allowed = true } }
	if !allowed { return store.ExecutionSession{}, store.ErrConflict }
	s.session.Status = tr.Status; s.session.ExitCode = tr.ExitCode; return s.session, nil
}
func (s *executionSessionStoreFake) UpdateRuntimeInstanceRunnerStatus(_ context.Context, _, _ string, status string) (store.RuntimeInstance, error) {
	s.mu.Lock(); defer s.mu.Unlock(); s.instance.RunnerStatus = status; return s.instance, nil
}

type fakeExecutionTransport struct {
	id        string
	stdout    string
	stderr    string
	resultCh  chan struct{}
	result    runner.Result
	waitErr   error
	terminated bool
	killed     bool
}
func newFakeExecutionTransport(id string) *fakeExecutionTransport { return &fakeExecutionTransport{id: id, resultCh: make(chan struct{})} }
func (t *fakeExecutionTransport) ID() string { return t.id }
func (t *fakeExecutionTransport) Stdout() io.Reader { return strings.NewReader(t.stdout) }
func (t *fakeExecutionTransport) Stderr() io.Reader { return strings.NewReader(t.stderr) }
func (t *fakeExecutionTransport) Stdin() io.WriteCloser { return nopBuffer{Buffer: &bytes.Buffer{}} }
func (t *fakeExecutionTransport) Wait(ctx context.Context) (runner.Result, error) { select { case <-t.resultCh: return t.result, t.waitErr; case <-ctx.Done(): return runner.Result{}, ctx.Err() } }
func (t *fakeExecutionTransport) Terminate(context.Context) error { t.terminated = true; return nil }
func (t *fakeExecutionTransport) Kill(context.Context) error { t.killed = true; return nil }
type nopBuffer struct{ *bytes.Buffer }
func (nopBuffer) Close() error { return nil }

type fakeExecutionClient struct {
	transport runner.ProcessSession
	startErr  error
	done      chan struct{}
}
func (c *fakeExecutionClient) Capabilities() protocol.Capabilities { return protocol.Capabilities{MaxActiveSessions: 1} }
func (c *fakeExecutionClient) Health() protocol.Health { return protocol.Health{Status: "ok"} }
func (c *fakeExecutionClient) Start(context.Context, string, runner.Request) (runner.ProcessSession, error) { return c.transport, c.startErr }
func (c *fakeExecutionClient) Attach(string) (runner.ProcessSession, error) { return c.transport, nil }
func (c *fakeExecutionClient) Done() <-chan struct{} { return c.done }
func (c *fakeExecutionClient) Err() error { return nil }
func (c *fakeExecutionClient) Close() error { return nil }

type fakeExecutionManager struct { client runner.Client; err error }
func (m *fakeExecutionManager) Connect(context.Context, string, string) (runner.Client, error) { return m.client, m.err }
func (m *fakeExecutionManager) Reconcile(context.Context, string, string, string) (runner.ProcessSession, bool, error) { return nil, false, nil }

func executionServiceFixture(t *testing.T) (*ExecutionSessionService, *executionSessionStoreFake, *fakeExecutionTransport) {
	t.Helper()
	transport := newFakeExecutionTransport("session-1")
	client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
	storeFake := &executionSessionStoreFake{
		run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING", RunnerStatus: "READY"},
	}
	service, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client})
	if err != nil { t.Fatal(err) }
	return service, storeFake, transport
}

func TestExecutionSessionServiceStartsAndPersistsCompletion(t *testing.T) {
	service, storeFake, transport := executionServiceFixture(t)
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"sh", "-c", "exit 5"}, CWD: "/workspace/sub"})
	if err != nil { t.Fatal(err) }
	if process.Record().Status != "RUNNING" || storeFake.instance.RunnerStatus != "BUSY" { t.Fatalf("record=%+v instance=%+v", process.Record(), storeFake.instance) }
	var argv []string
	if err := json.Unmarshal(storeFake.session.CommandArgv, &argv); err != nil || len(argv) != 3 { t.Fatalf("argv=%v err=%v", argv, err) }
	transport.result = runner.Result{ExitCode: 5}
	close(transport.resultCh)
	result, err := process.Wait(context.Background())
	if err != nil || result.ExitCode != 5 || process.Record().Status != "COMPLETED" || storeFake.instance.RunnerStatus != "READY" { t.Fatalf("result=%+v record=%+v instance=%+v err=%v", result, process.Record(), storeFake.instance, err) }
}

func TestExecutionSessionDisconnectRemainsNonTerminal(t *testing.T) {
	service, _, transport := executionServiceFixture(t)
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"sleep", "10"}})
	if err != nil { t.Fatal(err) }
	transport.waitErr = runner.ErrDisconnected
	close(transport.resultCh)
	_, err = process.Wait(context.Background())
	if !errors.Is(err, runner.ErrDisconnected) || process.Record().Status != "RUNNING" { t.Fatalf("record=%+v err=%v", process.Record(), err) }
}

func TestExecutionSessionCancellationTargetsSession(t *testing.T) {
	service, _, transport := executionServiceFixture(t)
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"sleep", "10"}})
	if err != nil { t.Fatal(err) }
	if err := process.Terminate(context.Background()); err != nil || !transport.terminated { t.Fatalf("Terminate() err=%v called=%v", err, transport.terminated) }
	transport.result = runner.Result{ExitCode: 143, Signaled: true}
	close(transport.resultCh)
	_, err = process.Wait(context.Background())
	if err != nil || process.Record().Status != "CANCELLED" { t.Fatalf("record=%+v err=%v", process.Record(), err) }
}

func TestExecutionSessionRejectsWorkspaceMismatchBeforeLaunch(t *testing.T) {
	service, storeFake, _ := executionServiceFixture(t)
	storeFake.instance.WorkspaceID = "other"
	_, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}})
	if err == nil || storeFake.session.ID != "" { t.Fatalf("session=%+v err=%v", storeFake.session, err) }
}
