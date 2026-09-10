package app

import (
	"fmt"
	"time"
)

const DefaultRunnerReconnectTimeout = 5 * time.Minute

type runnerReconnectTimeoutRegistry struct {
	RunnerRegistry
	timeout time.Duration
}

func (r *runnerReconnectTimeoutRegistry) DisconnectedSince(id string) (time.Time, bool) {
	tracker, ok := r.RunnerRegistry.(runnerDisconnectTracker)
	if !ok {
		return time.Time{}, false
	}
	return tracker.DisconnectedSince(id)
}

func (r *runnerReconnectTimeoutRegistry) reconnectTimeout() time.Duration {
	return r.timeout
}

func (s *ExecutionSessionService) SetRunnerReconnectTimeout(timeout time.Duration) error {
	if s == nil {
		return fmt.Errorf("execution session service is required")
	}
	if timeout <= 0 {
		return fmt.Errorf("runner reconnect timeout must be positive")
	}
	registry := s.registry
	if configured, ok := registry.(*runnerReconnectTimeoutRegistry); ok {
		registry = configured.RunnerRegistry
	}
	s.registry = &runnerReconnectTimeoutRegistry{RunnerRegistry: registry, timeout: timeout}
	return nil
}

func (s *ExecutionSessionService) runnerReconnectTimeout() time.Duration {
	if s != nil {
		if configured, ok := s.registry.(interface{ reconnectTimeout() time.Duration }); ok {
			return configured.reconnectTimeout()
		}
	}
	return DefaultRunnerReconnectTimeout
}

func (s *Services) ConfigureRunnerReconnectTimeout(timeout time.Duration) error {
	if s == nil || s.ExecutionSessions == nil || s.ExecutionSessions.sessions == nil {
		return fmt.Errorf("execution session service is unavailable")
	}
	return s.ExecutionSessions.sessions.SetRunnerReconnectTimeout(timeout)
}
