package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

var executionSessionStatuses = map[string]struct{}{
	"PENDING":   {},
	"STARTING":  {},
	"RUNNING":   {},
	"COMPLETED": {},
	"FAILED":    {},
	"CANCELLED": {},
}

func (s *Store) GetExecutionSession(ctx context.Context, projectID, sessionID string) (store.ExecutionSession, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(sessionID) == "" {
		return store.ExecutionSession{}, store.ErrInvalidArgument
	}
	return scanExecutionSession(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, run_id::text, runtime_instance_id::text, status, cwd, command_argv, exit_code, created_at, started_at, completed_at, updated_at
		FROM execution_sessions
		WHERE project_id = $1 AND id = $2
	`, projectID, sessionID))
}

func (s *Store) ListExecutionSessions(ctx context.Context, projectID string, statuses []string) ([]store.ExecutionSession, error) {
	return s.listExecutionSessions(ctx, projectID, "", statuses)
}

func (s *Store) ListExecutionSessionsByRuntimeInstance(ctx context.Context, projectID, runtimeInstanceID string, statuses []string) ([]store.ExecutionSession, error) {
	if strings.TrimSpace(runtimeInstanceID) == "" {
		return nil, store.ErrInvalidArgument
	}
	return s.listExecutionSessions(ctx, projectID, runtimeInstanceID, statuses)
}

func (s *Store) listExecutionSessions(ctx context.Context, projectID, runtimeInstanceID string, statuses []string) ([]store.ExecutionSession, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, store.ErrInvalidArgument
	}
	for _, status := range statuses {
		if _, ok := executionSessionStatuses[status]; !ok {
			return nil, store.ErrInvalidArgument
		}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, project_id::text, run_id::text, runtime_instance_id::text, status, cwd, command_argv, exit_code, created_at, started_at, completed_at, updated_at
		FROM execution_sessions
		WHERE project_id = $1
		  AND (nullif($2, '')::uuid IS NULL OR runtime_instance_id = nullif($2, '')::uuid)
		  AND (coalesce(cardinality($3::text[]), 0) = 0 OR status = ANY($3::text[]))
		ORDER BY created_at, id
	`, projectID, runtimeInstanceID, statuses)
	if err != nil {
		return nil, notFound(err)
	}
	defer rows.Close()
	sessions := make([]store.ExecutionSession, 0)
	for rows.Next() {
		session, err := scanExecutionSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *Store) TransitionExecutionSession(ctx context.Context, transition store.ExecutionSessionTransition) (store.ExecutionSession, error) {
	if strings.TrimSpace(transition.ProjectID) == "" || strings.TrimSpace(transition.SessionID) == "" || len(transition.FromStatuses) == 0 {
		return store.ExecutionSession{}, store.ErrInvalidArgument
	}
	if _, ok := executionSessionStatuses[transition.Status]; !ok {
		return store.ExecutionSession{}, store.ErrInvalidArgument
	}
	for _, status := range transition.FromStatuses {
		if _, ok := executionSessionStatuses[status]; !ok {
			return store.ExecutionSession{}, store.ErrInvalidArgument
		}
	}
	value, err := scanExecutionSession(s.pool.QueryRow(ctx, `
		UPDATE execution_sessions
		SET status = $3,
		    exit_code = CASE WHEN $4::integer IS NULL THEN exit_code ELSE $4 END,
		    started_at = CASE WHEN $3 = 'RUNNING' AND started_at IS NULL THEN now() ELSE started_at END,
		    completed_at = CASE WHEN $3 IN ('COMPLETED', 'FAILED', 'CANCELLED') AND completed_at IS NULL THEN now() ELSE completed_at END,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		  AND status = ANY($5::text[])
		RETURNING id::text, project_id::text, run_id::text, runtime_instance_id::text, status, cwd, command_argv, exit_code, created_at, started_at, completed_at, updated_at
	`, transition.ProjectID, transition.SessionID, transition.Status, transition.ExitCode, transition.FromStatuses))
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.ExecutionSession{}, err
	}
	if _, getErr := s.GetExecutionSession(ctx, transition.ProjectID, transition.SessionID); getErr == nil {
		return store.ExecutionSession{}, store.ErrConflict
	} else if !errors.Is(getErr, store.ErrNotFound) {
		return store.ExecutionSession{}, getErr
	}
	return store.ExecutionSession{}, store.ErrNotFound
}
