package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const executionSessionSelect = `
		SELECT id::text, project_id::text, run_id::text, runner_id::text, status, cwd, command_argv, exit_code, created_at, started_at, completed_at, updated_at`

const executionSessionReturning = `id::text, project_id::text, run_id::text, runner_id::text, status, cwd, command_argv, exit_code, created_at, started_at, completed_at, updated_at`

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
	return scanExecutionSession(s.pool.QueryRow(ctx, executionSessionSelect+`
		FROM execution_sessions
		WHERE project_id = $1 AND id = $2
	`, projectID, sessionID))
}

func (s *Store) ListExecutionSessions(ctx context.Context, projectID string, statuses []string) ([]store.ExecutionSession, error) {
	return s.listExecutionSessions(ctx, projectID, "", statuses)
}

func (s *Store) ListExecutionSessionsByRun(ctx context.Context, projectID, runID string, statuses []string) ([]store.ExecutionSession, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, store.ErrInvalidArgument
	}
	return s.listExecutionSessions(ctx, projectID, runID, statuses)
}

func (s *Store) ListExecutionSessionsByRunner(ctx context.Context, runnerID string, statuses []string) ([]store.ExecutionSession, error) {
	if strings.TrimSpace(runnerID) == "" {
		return nil, store.ErrInvalidArgument
	}
	if err := validateExecutionSessionStatuses(statuses); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, executionSessionSelect+`
		FROM execution_sessions
		WHERE runner_id = $1
		  AND (coalesce(cardinality($2::text[]), 0) = 0 OR status = ANY($2::text[]))
		ORDER BY created_at, id
	`, runnerID, statuses)
	if err != nil {
		return nil, notFound(err)
	}
	defer rows.Close()
	return scanExecutionSessions(rows)
}

func (s *Store) listExecutionSessions(ctx context.Context, projectID, runID string, statuses []string) ([]store.ExecutionSession, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, store.ErrInvalidArgument
	}
	if err := validateExecutionSessionStatuses(statuses); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, executionSessionSelect+`
		FROM execution_sessions
		WHERE project_id = $1
		  AND (nullif($2, '')::uuid IS NULL OR run_id = nullif($2, '')::uuid)
		  AND (coalesce(cardinality($3::text[]), 0) = 0 OR status = ANY($3::text[]))
		ORDER BY created_at, id
	`, projectID, runID, statuses)
	if err != nil {
		return nil, notFound(err)
	}
	defer rows.Close()
	return scanExecutionSessions(rows)
}

func validateExecutionSessionStatuses(statuses []string) error {
	for _, status := range statuses {
		if _, ok := executionSessionStatuses[status]; !ok {
			return store.ErrInvalidArgument
		}
	}
	return nil
}

func scanExecutionSessions(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]store.ExecutionSession, error) {
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
	if err := validateExecutionSessionStatuses(transition.FromStatuses); err != nil {
		return store.ExecutionSession{}, err
	}
	value, err := scanExecutionSession(s.pool.QueryRow(ctx, `
		UPDATE execution_sessions
		SET status = $3,
		    exit_code = CASE WHEN $4::integer IS NULL THEN exit_code ELSE $4 END,
		    command_argv = CASE WHEN $6::jsonb IS NULL THEN command_argv ELSE $6 END,
		    started_at = CASE WHEN $3 = 'RUNNING' AND started_at IS NULL THEN now() ELSE started_at END,
		    completed_at = CASE WHEN $3 IN ('COMPLETED', 'FAILED', 'CANCELLED') AND completed_at IS NULL THEN now() ELSE completed_at END,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		  AND status = ANY($5::text[])
		RETURNING `+executionSessionReturning+`
	`, transition.ProjectID, transition.SessionID, transition.Status, transition.ExitCode, transition.FromStatuses, commandArgvJSON(transition.CommandArgv)))
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
