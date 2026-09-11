package app

import (
	"fmt"
	"time"
)

const DefaultRunnerReconnectTimeout = 5 * time.Minute

func (s *ExecutionSessionService) SetRunnerReconnectTimeout(timeout time.Duration) error {
	if s == nil {
		return fmt.Errorf("execution session service is required")
	}
	if timeout <= 0 {
		return fmt.Errorf("runner reconnect timeout must be positive")
	}
	s.reconnectTimeoutNanos.Store(int64(timeout))
	return nil
}

func (s *ExecutionSessionService) runnerReconnectTimeout() time.Duration {
	if s == nil {
		return DefaultRunnerReconnectTimeout
	}
	if timeout := time.Duration(s.reconnectTimeoutNanos.Load()); timeout > 0 {
		return timeout
	}
	return DefaultRunnerReconnectTimeout
}

func (s *Services) ConfigureRunnerReconnectTimeout(timeout time.Duration) error {
	if s == nil || s.ExecutionSessions == nil || s.ExecutionSessions.sessions == nil {
		return fmt.Errorf("execution session service is unavailable")
	}
	return s.ExecutionSessions.sessions.SetRunnerReconnectTimeout(timeout)
}
