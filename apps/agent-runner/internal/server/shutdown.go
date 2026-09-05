package server

import (
	"context"
	"errors"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/session"
	"github.com/gorilla/websocket"
)

const shutdownGracePeriod = 500 * time.Millisecond

var errServerShuttingDown = errors.New("runner is shutting down")

// Shutdown stops accepting runner work, closes hijacked WebSocket connections,
// terminates active sessions, and waits for runner-owned goroutines to finish.
func (s *Server) Shutdown(ctx context.Context) error {
	s.lifecycleMu.Lock()
	s.shuttingDown = true
	connections := make([]*websocket.Conn, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.lifecycleMu.Unlock()

	s.shutdownCancel()
	for _, conn := range connections {
		_ = conn.Close()
	}

	s.signalActiveSessions(false)
	if !s.waitForNoSessions(ctx, shutdownGracePeriod) {
		s.signalActiveSessions(true)
		if !s.waitForNoSessions(ctx, 0) {
			return ctx.Err()
		}
	}

	return s.waitForLifecycle(ctx)
}

func (s *Server) registerConnection(conn *websocket.Conn) bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.shuttingDown {
		return false
	}
	s.connections[conn] = struct{}{}
	s.handlerWG.Add(1)
	return true
}

func (s *Server) unregisterConnection(conn *websocket.Conn) {
	s.lifecycleMu.Lock()
	delete(s.connections, conn)
	s.lifecycleMu.Unlock()
	s.handlerWG.Done()
}

func (s *Server) startSession(id string, request session.Request) (*session.Session, *stdinPump, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.shuttingDown {
		return nil, nil, errServerShuttingDown
	}

	execution, err := s.manager.Start(id, request)
	if err != nil {
		return nil, nil, err
	}
	stdin := newStdinPump(execution.Stdin())
	s.stdinMu.Lock()
	s.stdinPumps[id] = stdin
	s.stdinMu.Unlock()
	s.streamWG.Add(1)
	return execution, stdin, nil
}

func (s *Server) signalActiveSessions(force bool) {
	for _, id := range s.manager.ActiveIDs() {
		execution, err := s.manager.Get(id)
		if err != nil {
			continue
		}
		if force {
			_ = execution.Kill()
		} else {
			_ = execution.Terminate()
		}
	}
}

func (s *Server) waitForNoSessions(ctx context.Context, timeout time.Duration) bool {
	if s.manager.ActiveCount() == 0 {
		return true
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var timeoutC <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timeoutC = timer.C
		defer timer.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return false
		case <-timeoutC:
			return false
		case <-ticker.C:
			if s.manager.ActiveCount() == 0 {
				return true
			}
		}
	}
}

func (s *Server) waitForLifecycle(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.handlerWG.Wait()
		s.streamWG.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		s.signalActiveSessions(true)
		return ctx.Err()
	case <-done:
		return nil
	}
}
