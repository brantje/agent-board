package app

import (
	"context"
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
}
