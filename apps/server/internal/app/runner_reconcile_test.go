package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestReconcileRunnerSessionReturnsUncertainWhileReconnectWindowOpen(t *testing.T) {
	manager := &reconcileExecutionManager{err: runner.ErrDisconnected}
	storeFake := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}
	service, err := NewExecutionSessionService(storeFake, manager, manager)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Reconcile(context.Background(), "project-1", "session-1")
	if err == nil {
		t.Fatal("expected uncertain reconciliation error")
	}
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "execution_session_uncertain" {
		t.Fatalf("unexpected error %#v", err)
	}
}

func TestReconcileRunnerSessionFailsAfterReconnectTimeout(t *testing.T) {
	original := runnerReconnectTimeout
	runnerReconnectTimeout = time.Millisecond
	defer func() { runnerReconnectTimeout = original }()

	manager := &reconcileExecutionManager{err: runner.ErrDisconnected}
	storeFake := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now().Add(-time.Second),
		},
	}
	service, err := NewExecutionSessionService(storeFake, manager, manager)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Reconcile(context.Background(), "project-1", "session-1"); err != nil {
		t.Fatalf("reconcile error=%v", err)
	}
	if storeFake.session.Status != "FAILED" {
		t.Fatalf("session status=%q", storeFake.session.Status)
	}
}

func TestTerminateRunnerSessionsFailsActiveSessions(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	manager := &reconcileExecutionManager{transport: transport, active: true}
	storeFake := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}
	service, err := NewExecutionSessionService(storeFake, manager, manager)
	if err != nil {
		t.Fatal(err)
	}
	service.retainExecutionProcess(storeFake.session, transport)

	if err := service.TerminateRunnerSessions(context.Background(), "runner-1"); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if storeFake.session.Status != "FAILED" {
		t.Fatalf("session status=%q", storeFake.session.Status)
	}
	if !transport.killed {
		t.Fatal("expected live runner session to be killed")
	}
}

func TestReconcileRunnerSessionReattachesActiveTransport(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	manager := &reconcileExecutionManager{transport: transport, active: true}
	storeFake := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}
	service, err := NewExecutionSessionService(storeFake, manager, manager)
	if err != nil {
		t.Fatal(err)
	}

	process, err := service.Reconcile(context.Background(), "project-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if process == nil || process.ID() != "session-1" {
		t.Fatalf("unexpected process %#v", process)
	}

	again, err := service.Reconcile(context.Background(), "project-1", "session-1")
	if err != nil || again != process {
		t.Fatalf("live process was not reused: %#v err=%v", again, err)
	}
}

func TestReconcileRunnerSessionPromotesStartingAndCompletesInactiveTransport(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	close(transport.resultCh)
	transport.result = runner.Result{ExitCode: 3}
	manager := &reconcileExecutionManager{transport: transport, active: true}
	storeFake := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "STARTING", UpdatedAt: time.Now(),
		},
	}
	service, err := NewExecutionSessionService(storeFake, manager, manager)
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.Reconcile(context.Background(), "project-1", "session-1")
	if err != nil || process == nil || storeFake.session.Status != "RUNNING" {
		t.Fatalf("starting promote process=%v status=%q err=%v", process, storeFake.session.Status, err)
	}

	completed := newFakeExecutionTransport("session-2")
	completed.result = runner.Result{ExitCode: 0}
	close(completed.resultCh)
	doneStore := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-2", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}
	doneService, err := NewExecutionSessionService(doneStore, &reconcileExecutionManager{transport: completed, active: false}, &reconcileExecutionManager{transport: completed, active: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doneService.Reconcile(context.Background(), "project-1", "session-2"); err != nil {
		t.Fatal(err)
	}
	if doneStore.session.Status != "COMPLETED" {
		t.Fatalf("inactive completed status=%q", doneStore.session.Status)
	}

	nilService, err := NewExecutionSessionService(&executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-3", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}, &reconcileExecutionManager{}, &reconcileExecutionManager{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nilService.Reconcile(context.Background(), "project-1", "session-3"); err == nil {
		t.Fatal("nil transport accepted")
	}
	started := time.Now()
	if got := sessionAnchorTime(store.ExecutionSession{StartedAt: &started, UpdatedAt: started.Add(-time.Hour)}); !got.Equal(started) {
		t.Fatalf("anchor=%v", got)
	}
}

func TestReconcileRunnerSessionInactiveWaitOutcomes(t *testing.T) {
	disconnected := newFakeExecutionTransport("session-disconnected")
	disconnected.waitErr = runner.ErrDisconnected
	close(disconnected.resultCh)
	disconnectedStore := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-disconnected", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}
	disconnectedService, err := NewExecutionSessionService(disconnectedStore, &reconcileExecutionManager{transport: disconnected}, &reconcileExecutionManager{transport: disconnected})
	if err != nil {
		t.Fatal(err)
	}
	_, err = disconnectedService.Reconcile(context.Background(), "project-1", "session-disconnected")
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "execution_session_uncertain" {
		t.Fatalf("disconnected wait err=%v", err)
	}

	failed := newFakeExecutionTransport("session-failed")
	failed.waitErr = errors.New("wait failed")
	close(failed.resultCh)
	failedStore := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-failed", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}
	failedService, err := NewExecutionSessionService(failedStore, &reconcileExecutionManager{transport: failed}, &reconcileExecutionManager{transport: failed})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failedService.Reconcile(context.Background(), "project-1", "session-failed"); err == nil {
		t.Fatal("failed wait accepted")
	}
	if failedStore.session.Status != "FAILED" {
		t.Fatalf("failed wait status=%q", failedStore.session.Status)
	}

	cancelled := newFakeExecutionTransport("session-cancelled")
	cancelledStore := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-cancelled", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}
	cancelledService, err := NewExecutionSessionService(cancelledStore, &reconcileExecutionManager{transport: cancelled}, &reconcileExecutionManager{transport: cancelled})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cancelledService.Reconcile(ctx, "project-1", "session-cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait err=%v", err)
	}

	timedOut := newFakeExecutionTransport("session-timeout")
	timeoutStore := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-timeout", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}
	timeoutService, err := NewExecutionSessionService(timeoutStore, &reconcileExecutionManager{transport: timedOut}, &reconcileExecutionManager{transport: timedOut})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := timeoutService.Reconcile(context.Background(), "project-1", "session-timeout"); err != nil {
		t.Fatalf("timeout wait err=%v", err)
	}
	if timeoutStore.session.Status != "FAILED" {
		t.Fatalf("timeout wait status=%q", timeoutStore.session.Status)
	}

	closed := newFakeExecutionTransport("session-closed")
	closed.waitErr = runner.ErrClosed
	close(closed.resultCh)
	closedStore := &executionSessionStoreFake{
		session: store.ExecutionSession{
			ID: "session-closed", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1",
			Status: "RUNNING", UpdatedAt: time.Now(),
		},
	}
	closedService, err := NewExecutionSessionService(closedStore, &reconcileExecutionManager{transport: closed}, &reconcileExecutionManager{transport: closed})
	if err != nil {
		t.Fatal(err)
	}
	_, err = closedService.Reconcile(context.Background(), "project-1", "session-closed")
	appErr, ok = err.(*Error)
	if !ok || appErr.Code != "execution_session_uncertain" {
		t.Fatalf("closed wait err=%v", err)
	}
}
