package app

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

// DialSession opens a connection from the Runner that owns sessionID to a
// service local to that active Execution Session.
func (s *ExecutionSessionService) DialSession(ctx context.Context, projectID, sessionID, network, address string) (net.Conn, error) {
	if s == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, NewError("invalid_argument", "projectId and sessionId are required", store.ErrInvalidArgument)
	}
	session, err := s.store.GetExecutionSession(ctx, projectID, sessionID)
	if err != nil {
		return nil, translateStoreError(err, "execution_session")
	}
	if session.Status != "RUNNING" {
		return nil, NewError("execution_session_not_running", "Execution Session is not running", store.ErrConflict)
	}
	if strings.TrimSpace(session.RunnerID) == "" {
		return nil, NewError("execution_session_runner_missing", "Execution Session has no Runner binding", store.ErrInvalidArgument)
	}

	client, err := s.registry.Connect(ctx, projectID, session.RunnerID)
	if err != nil {
		return nil, fmt.Errorf("connect runner for session-local dial: %w", err)
	}
	dialer, ok := client.(runner.SessionDialer)
	if !ok {
		return nil, NewError("runner_session_connect_unsupported", "Runner does not support session-local connections", runner.ErrSessionConnectUnsupported)
	}
	conn, err := dialer.DialSession(ctx, sessionID, network, address)
	if err != nil {
		return nil, fmt.Errorf("dial session-local service: %w", err)
	}
	return conn, nil
}

// DialSession preserves the trusted execution boundary while forwarding the
// already-scoped request to the low-level Execution Session service.
func (s *AuthorizedExecutionSessionService) DialSession(ctx context.Context, projectID, sessionID, network, address string) (net.Conn, error) {
	if s == nil || s.sessions == nil {
		return nil, NewError("execution_session_unavailable", "Execution Session service is unavailable", store.ErrInvalidArgument)
	}
	return s.sessions.DialSession(ctx, projectID, sessionID, network, address)
}
