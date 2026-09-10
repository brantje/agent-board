package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func TestReconcileRunnerConnectionIdleReplacementAllowed(t *testing.T) {
	service, _, _ := runnerOwnedExecutionService(t)
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", protocol.Health{Status: "ok"}, protocol.Health{Status: "ok"}); err != nil {
		t.Fatalf("idle replacement rejected: %v", err)
	}
}

func TestReconcileRunnerConnectionSameActiveSessionAllowed(t *testing.T) {
	service, storeFake, _ := runnerOwnedExecutionService(t)
	storeFake.session = store.ExecutionSession{ID: "session-x", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: "RUNNING"}
	health := protocol.Health{Status: "ok", ActiveSessions: 1, ActiveSessionIDs: []string{"session-x"}}
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", health, health); err != nil {
		t.Fatalf("same-session replacement rejected: %v", err)
	}
}

func TestReconcileRunnerConnectionDoesNotLoseActiveSession(t *testing.T) {
	service, storeFake, _ := runnerOwnedExecutionService(t)
	storeFake.session = store.ExecutionSession{ID: "session-x", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: "RUNNING"}
	current := protocol.Health{Status: "ok", ActiveSessions: 1, ActiveSessionIDs: []string{"session-x"}}
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", current, protocol.Health{Status: "ok"}); err == nil {
		t.Fatal("replacement that dropped durable active session was accepted")
	}
}

func TestReconcileRunnerConnectionRejectsUnexpectedActiveSession(t *testing.T) {
	service, _, _ := runnerOwnedExecutionService(t)
	replacement := protocol.Health{Status: "ok", ActiveSessions: 1, ActiveSessionIDs: []string{"unexpected"}}
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", protocol.Health{Status: "ok"}, replacement); err == nil {
		t.Fatal("unexpected active session claim was accepted")
	}
}

func TestReconcileRunnerConnectionRejectsInconsistentHealth(t *testing.T) {
	service, _, _ := runnerOwnedExecutionService(t)
	if err := service.ReconcileRunnerConnection(context.Background(), "runner-1", protocol.Health{Status: "ok"}, protocol.Health{Status: "ok", ActiveSessions: 1}); err == nil {
		t.Fatal("inconsistent active-session health was accepted")
	}
}
