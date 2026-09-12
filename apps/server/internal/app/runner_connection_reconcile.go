package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func (s *ExecutionSessionService) ReconcileRunnerConnection(ctx context.Context, runnerID string, current, replacement protocol.Health) error {
	if s == nil || strings.TrimSpace(runnerID) == "" {
		return fmt.Errorf("runner connection reconciliation requires a runner id")
	}
	currentIDs, err := runnerHealthSessions(current)
	if err != nil {
		return fmt.Errorf("current runner health: %w", err)
	}
	replacementIDs, err := runnerHealthSessions(replacement)
	if err != nil {
		return fmt.Errorf("replacement runner health: %w", err)
	}

	sessions, err := s.store.ListExecutionSessionsByRunner(ctx, runnerID, []string{"STARTING", "RUNNING"})
	if err != nil {
		return translateStoreError(err, "execution_session")
	}
	expected := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		expected[session.ID] = struct{}{}
	}

	for _, id := range currentIDs {
		if _, ok := expected[id]; !ok {
			return NewError("runner_session_conflict", "current Runner transport reports an unexpected active Execution Session", store.ErrConflict)
		}
	}
	if len(expected) == 0 {
		if len(replacementIDs) > 0 {
			return NewError("runner_session_conflict", "replacement Runner reports an unauthorized active Execution Session", store.ErrConflict)
		}
		return nil
	}
	for _, id := range currentIDs {
		if !slices.Contains(replacementIDs, id) {
			return NewError("runner_session_conflict", "replacement Runner did not report the durable active Execution Session", store.ErrConflict)
		}
	}
	for _, id := range replacementIDs {
		if _, ok := expected[id]; !ok {
			return NewError("runner_session_conflict", "replacement Runner reports an unauthorized active Execution Session", store.ErrConflict)
		}
	}
	return nil
}

func runnerHealthSessions(health protocol.Health) ([]string, error) {
	if health.ActiveSessions < 0 {
		return nil, fmt.Errorf("active_sessions must be non-negative")
	}
	if health.ActiveSessions != len(health.ActiveSessionIDs) {
		return nil, fmt.Errorf("active_sessions does not match active_session_ids")
	}
	ids := make([]string, 0, len(health.ActiveSessionIDs))
	for _, id := range health.ActiveSessionIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("active session id is required")
		}
		ids = append(ids, id)
	}
	return ids, nil
}
