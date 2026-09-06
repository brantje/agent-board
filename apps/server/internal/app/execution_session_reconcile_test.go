package app

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reconcileExecutionStore struct {
	*executionSessionStoreFake
	projects []store.Project
}

func (s *reconcileExecutionStore) ListProjects(context.Context) ([]store.Project, error) {
	return append([]store.Project(nil), s.projects...), nil
}
func (s *reconcileExecutionStore) ListExecutionSessions(_ context.Context, projectID string, statuses []string) ([]store.ExecutionSession, error) {
	if s.session.ID == "" || s.session.ProjectID != projectID {
		return nil, nil
	}
	for _, status := range statuses {
		if s.session.Status == status {
			return []store.ExecutionSession{s.session}, nil
		}
	}
	return nil, nil
}

type reconcileExecutionManager struct {
	transport runner.ProcessSession
	active    bool
	err       error
}
func (m *reconcileExecutionManager) Connect(context.Context, string, string) (runner.Client, error) { return nil, errors.New("not used") }
func (m *reconcileExecutionManager) Reconcile(context.Context, string, string, string) (runner.ProcessSession, bool, error) {
	return m.transport, m.active, m.err
}

func TestReconcileRestoresLiveSessionWithoutStartingDuplicate(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	base := &executionSessionStoreFake{
		run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING", RunnerStatus: "UNAVAILABLE"},
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "STARTING"},
	}
	service, err := NewExecutionSessionService(base, &reconcileExecutionManager{transport: transport, active: true})
	if err != nil { t.Fatal(err) }
	process, err := service.Reconcile(context.Background(), "project-1", "session-1")
	if err != nil || process == nil || process.Record().Status != "RUNNING" || base.instance.RunnerStatus != "BUSY" {
		t.Fatalf("process=%v session=%+v instance=%+v err=%v", process, base.session, base.instance, err)
	}
	transport.result = runner.Result{ExitCode: 0}
	close(transport.resultCh)
	_, _ = process.Wait(context.Background())
}

type reconcileStatusFailureStore struct {
	*executionSessionStoreFake
	err error
}

func (s *reconcileStatusFailureStore) UpdateRuntimeInstanceRunnerStatus(context.Context, string, string, string) (store.RuntimeInstance, error) {
	return s.instance, s.err
}

type readSignalReader struct {
	once sync.Once
	read chan struct{}
}

func newReadSignalReader() *readSignalReader {
	return &readSignalReader{read: make(chan struct{})}
}

func (r *readSignalReader) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.read) })
	return 0, io.EOF
}

type reconcileDrainTransport struct {
	*fakeExecutionTransport
	stdout *readSignalReader
	stderr *readSignalReader
}

func (t *reconcileDrainTransport) Stdout() io.Reader { return t.stdout }
func (t *reconcileDrainTransport) Stderr() io.Reader { return t.stderr }

func TestReconcileDrainsAttachedOutputWhenBusyPersistenceFails(t *testing.T) {
	statusErr := errors.New("persist BUSY failed")
	base := &executionSessionStoreFake{
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING", RunnerStatus: "READY"},
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"},
	}
	storeWithFailure := &reconcileStatusFailureStore{executionSessionStoreFake: base, err: statusErr}
	transport := &reconcileDrainTransport{
		fakeExecutionTransport: newFakeExecutionTransport("session-1"),
		stdout:                 newReadSignalReader(),
		stderr:                 newReadSignalReader(),
	}
	service, err := NewExecutionSessionService(storeWithFailure, &reconcileExecutionManager{transport: transport, active: true})
	if err != nil {
		t.Fatal(err)
	}

	process, err := service.Reconcile(context.Background(), "project-1", "session-1")
	if err == nil || process != nil {
		t.Fatalf("process=%v err=%v", process, err)
	}
	for name, drained := range map[string]<-chan struct{}{"stdout": transport.stdout.read, "stderr": transport.stderr.read} {
		select {
		case <-drained:
		case <-time.After(time.Second):
			t.Fatalf("%s was not drained after BUSY persistence failure", name)
		}
	}
}

func TestReconcileConsumesRetainedTerminalResultBeforeFailing(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	transport.result = runner.Result{ExitCode: 23}
	close(transport.resultCh)
	base := &executionSessionStoreFake{
		run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING", RunnerStatus: "UNAVAILABLE"},
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"},
	}
	service, _ := NewExecutionSessionService(base, &reconcileExecutionManager{transport: transport, active: false})
	process, err := service.Reconcile(context.Background(), "project-1", "session-1")
	if err != nil || process != nil || base.session.Status != "COMPLETED" || base.session.ExitCode == nil || *base.session.ExitCode != 23 {
		t.Fatalf("process=%v session=%+v err=%v", process, base.session, err)
	}
}

func TestReconcileTransportFailureLeavesSessionNonTerminal(t *testing.T) {
	base := &executionSessionStoreFake{
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING"},
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"},
	}
	service, _ := NewExecutionSessionService(base, &reconcileExecutionManager{err: runner.ErrDisconnected})
	_, err := service.Reconcile(context.Background(), "project-1", "session-1")
	if err == nil || base.session.Status != "RUNNING" {
		t.Fatalf("session=%+v err=%v", base.session, err)
	}
}

func TestReconcileCallerDeadlineLeavesSessionNonTerminal(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	base := &executionSessionStoreFake{
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING"},
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"},
	}
	service, _ := NewExecutionSessionService(base, &reconcileExecutionManager{transport: transport, active: false})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := service.Reconcile(ctx, "project-1", "session-1")
	if !errors.Is(err, context.DeadlineExceeded) || base.session.Status != "RUNNING" {
		t.Fatalf("session=%+v err=%v", base.session, err)
	}
}

func TestReconcileAllHandlesNeverStartedPendingSession(t *testing.T) {
	base := &executionSessionStoreFake{
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING"},
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "PENDING"},
	}
	wrapped := &reconcileExecutionStore{executionSessionStoreFake: base, projects: []store.Project{{ID: "project-1"}}}
	service, _ := NewExecutionSessionService(wrapped, &reconcileExecutionManager{})
	if err := service.ReconcileAll(context.Background()); err != nil || base.session.Status != "FAILED" {
		t.Fatalf("session=%+v err=%v", base.session, err)
	}
}
