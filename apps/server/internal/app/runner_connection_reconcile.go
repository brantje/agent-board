package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func (s *ExecutionSessionService) ReconcileRunnerConnection(ctx context.Context, runnerID string, current, replacement protocol.Health) error {
	if s == nil || strings.TrimSpace(runnerID) == "" {
		return fmt.Errorf("runner connection reconciliation requires a runner id")
	}
	currentSession, err := runnerHealthSession(current)
	if err != nil {
		return fmt.Errorf("current runner health: %w", err)
	}
	replacementSession, err := runnerHealthSession(replacement)
	if err != nil {
		return fmt.Errorf("replacement runner health: %w", err)
	}

	sessions, err := s.store.ListExecutionSessionsByRunner(ctx, runnerID, []string{"STARTING", "RUNNING"})
	if err != nil {
		return translateStoreError(err, "execution_session")
	}
	if len(sessions) > 1 {
		return NewError("runner_session_conflict", "runner has multiple active Execution Sessions", store.ErrConflict)
	}

	expected := ""
	if len(sessions) == 1 {
		expected = sessions[0].ID
	}
	if currentSession != "" && currentSession != expected {
		return NewError("runner_session_conflict", "current Runner transport reports an unexpected active Execution Session", store.ErrConflict)
	}
	if expected == "" {
		if replacementSession != "" {
			return NewError("runner_session_conflict", "replacement Runner reports an unauthorized active Execution Session", store.ErrConflict)
		}
		return nil
	}
	if replacementSession != expected {
		return NewError("runner_session_conflict", "replacement Runner did not report the durable active Execution Session", store.ErrConflict)
	}
	return nil
}

func runnerHealthSession(health protocol.Health) (string, error) {
	if health.ActiveSessions < 0 || health.ActiveSessions > 1 {
		return "", fmt.Errorf("active_sessions must be 0 or 1")
	}
	if health.ActiveSessions != len(health.ActiveSessionIDs) {
		return "", fmt.Errorf("active_sessions does not match active_session_ids")
	}
	if health.ActiveSessions == 0 {
		return "", nil
	}
	id := strings.TrimSpace(health.ActiveSessionIDs[0])
	if id == "" {
		return "", fmt.Errorf("active session id is required")
	}
	return id, nil
}
