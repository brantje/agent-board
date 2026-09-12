package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

type runnerReconcileRegistryFake struct {
	transport      runner.ProcessSession
	active         bool
	err            error
	disconnectedAt time.Time
	disconnected   bool
}

func (r *runnerReconcileRegistryFake) Connect(context.Context, string, string) (runner.Client, error) {
	return nil, errors.New("not used")
}

func (r *runnerReconcileRegistryFake) Reconcile(context.Context, string, string, string) (runner.ProcessSession, bool, error) {
	return r.transport, r.active, r.err
}

func (r *runnerReconcileRegistryFake) DisconnectedSince(string) (time.Time, bool) {
	return r.disconnectedAt, r.disconnected
}

func runnerOwnedSessionStore(status string) *executionSessionStoreFake {
	return &executionSessionStoreFake{session: store.ExecutionSession{
		ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: status,
	}}
}

func TestRunnerReconcileUsesBoundedReconnectWindow(t *testing.T) {
	registry := &runnerReconcileRegistryFake{
		err: runner.ErrDisconnected, disconnected: true, disconnectedAt: time.Now(),
	}
	storeFake := runnerOwnedSessionStore("RUNNING")
	service, err := NewExecutionSessionService(storeFake, &reconcileExecutionManager{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetRunnerReconnectTimeout(time.Minute); err != nil {
		t.Fatal(err)
	}

	_, err = service.Reconcile(context.Background(), "project-1", "session-1")
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "execution_session_uncertain" || storeFake.session.Status != "RUNNING" {
		t.Fatalf("open reconnect window: status=%q err=%v", storeFake.session.Status, err)
	}

	registry.disconnectedAt = time.Now().Add(-2 * time.Minute)
	if _, err := service.Reconcile(context.Background(), "project-1", "session-1"); err != nil {
		t.Fatalf("expired reconnect window: %v", err)
	}
	if storeFake.session.Status != "FAILED" {
		t.Fatalf("expired reconnect window status=%q", storeFake.session.Status)
	}
}

func TestRunnerReconcileReattachesOrConsumesTerminalResult(t *testing.T) {
	activeTransport := newFakeExecutionTransport("session-1")
	activeStore := runnerOwnedSessionStore("STARTING")
	activeService, err := NewExecutionSessionService(activeStore, &reconcileExecutionManager{}, &runnerReconcileRegistryFake{transport: activeTransport, active: true})
	if err != nil {
		t.Fatal(err)
	}
	process, err := activeService.Reconcile(context.Background(), "project-1", "session-1")
	if err != nil || process == nil || process.Record().Status != "RUNNING" {
		t.Fatalf("reattach process=%v session=%+v err=%v", process, activeStore.session, err)
	}
	if again, err := activeService.Reconcile(context.Background(), "project-1", "session-1"); err != nil || again != process {
		t.Fatalf("retained process not reused: process=%v err=%v", again, err)
	}

	terminalTransport := newFakeExecutionTransport("session-1")
	terminalTransport.result = runner.Result{ExitCode: 23}
	close(terminalTransport.resultCh)
	terminalStore := runnerOwnedSessionStore("RUNNING")
	terminalService, err := NewExecutionSessionService(terminalStore, &reconcileExecutionManager{}, &runnerReconcileRegistryFake{transport: terminalTransport})
	if err != nil {
		t.Fatal(err)
	}
	if process, err := terminalService.Reconcile(context.Background(), "project-1", "session-1"); err != nil || process != nil {
		t.Fatalf("terminal reconcile process=%v err=%v", process, err)
	}
	if terminalStore.session.Status != "COMPLETED" || terminalStore.session.ExitCode == nil || *terminalStore.session.ExitCode != 23 {
		t.Fatalf("terminal session=%+v", terminalStore.session)
	}
}

func TestTerminateRunnerSessionsKillsLiveProcessAndFailsSession(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	storeFake := runnerOwnedSessionStore("RUNNING")
	service, err := NewExecutionSessionService(storeFake, &reconcileExecutionManager{}, &runnerReconcileRegistryFake{})
	if err != nil {
		t.Fatal(err)
	}
	service.retainExecutionProcess(storeFake.session, transport)

	if err := service.TerminateRunnerSessions(context.Background(), "runner-1"); err != nil {
		t.Fatal(err)
	}
	if !transport.killed || storeFake.session.Status != "FAILED" {
		t.Fatalf("killed=%v session=%+v", transport.killed, storeFake.session)
	}
}

func TestRunnerConnectionReconcileFencesDurableSessionOwnership(t *testing.T) {
	storeFake := runnerOwnedSessionStore("RUNNING")
	service, err := NewExecutionSessionService(storeFake, &reconcileExecutionManager{}, &runnerReconcileRegistryFake{})
	if err != nil {
		t.Fatal(err)
	}
	health := protocol.Health{Status: "ok", ActiveSessions: 1, ActiveSessionIDs: []string{"session-1"}}
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", health, health); err != nil {
		t.Fatalf("matching durable session rejected: %v", err)
	}

	missing := protocol.Health{Status: "ok"}
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", health, missing); err == nil {
		t.Fatal("replacement without durable active session accepted")
	}
	invalid := protocol.Health{Status: "ok", ActiveSessions: 1}
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", invalid, health); err == nil {
		t.Fatal("invalid health accepted")
	}

	storeFake.session = store.ExecutionSession{}
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", missing, health); err == nil {
		t.Fatal("unauthorized replacement session accepted")
	}
}

func TestRunnerConnectionReconcileAllowsMultipleConcurrentSessions(t *testing.T) {
	storeFake := &executionSessionStoreFake{sessionsByRunner: []store.ExecutionSession{
		{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: "RUNNING"},
		{ID: "session-2", ProjectID: "project-1", RunID: "run-2", RunnerID: "runner-1", Status: "RUNNING"},
	}}
	service, err := NewExecutionSessionService(storeFake, &reconcileExecutionManager{}, &runnerReconcileRegistryFake{})
	if err != nil {
		t.Fatal(err)
	}
	current := protocol.Health{Status: "ok", ActiveSessions: 2, ActiveSessionIDs: []string{"session-1", "session-2"}}
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", current, current); err != nil {
		t.Fatalf("matching concurrent sessions rejected: %v", err)
	}
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", current, protocol.Health{Status: "ok", ActiveSessions: 1, ActiveSessionIDs: []string{"session-1"}}); err == nil {
		t.Fatal("replacement missing durable active session accepted")
	}
}

func TestRunnerReconnectTimeoutValidation(t *testing.T) {
	service, err := NewExecutionSessionService(runnerOwnedSessionStore("RUNNING"), &reconcileExecutionManager{}, &runnerReconcileRegistryFake{})
	if err != nil {
		t.Fatal(err)
	}
	if service.runnerReconnectTimeout() != DefaultRunnerReconnectTimeout {
		t.Fatalf("default timeout=%v", service.runnerReconnectTimeout())
	}
	if err := service.SetRunnerReconnectTimeout(0); err == nil {
		t.Fatal("non-positive reconnect timeout accepted")
	}
	if err := service.SetRunnerReconnectTimeout(250 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if service.runnerReconnectTimeout() != 250*time.Millisecond {
		t.Fatalf("configured timeout=%v", service.runnerReconnectTimeout())
	}
}
