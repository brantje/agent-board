package app

import (
	"context"
	"errors"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

var runnerReconnectTimeout = 5 * time.Minute

type runnerDisconnectTracker interface {
	DisconnectedSince(string) (time.Time, bool)
}

func (s *ExecutionSessionService) reconcileRunnerSession(ctx context.Context, session store.ExecutionSession) (*ExecutionProcess, error) {
	if process, ok := s.liveProcess(session.ProjectID, session.ID); ok {
		return process, nil
	}

	transport, active, err := s.registry.Reconcile(ctx, session.ProjectID, session.RunnerID, session.ID)
	if err != nil {
		if errors.Is(err, runner.ErrDisconnected) {
			return nil, s.failRunnerSessionAfterDisconnectTimeout(ctx, session)
		}
		return nil, NewError("execution_session_uncertain", "Execution Session could not be reconciled with its runner", err)
	}
	if transport == nil {
		return nil, s.failRunnerSessionAfterDisconnectTimeout(ctx, session)
	}
	if active {
		if session.Status == "STARTING" {
			session, err = s.transition(ctx, session, []string{"STARTING"}, "RUNNING", nil)
			if err != nil {
				return nil, err
			}
		}
		return newExecutionProcess(s, session, transport), nil
	}

	drainExecutionOutput(transport.Stdout(), transport.Stderr())
	waitCtx, cancel := context.WithTimeout(ctx, retainedTerminalDeliveryGrace)
	defer cancel()
	result, waitErr := transport.Wait(waitCtx)
	if waitErr == nil {
		exitCode := result.ExitCode
		_, err := s.transition(ctx, session, []string{"STARTING", "RUNNING"}, "COMPLETED", &exitCode)
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(waitErr, runner.ErrDisconnected) || errors.Is(waitErr, runner.ErrClosed) {
		return nil, NewError("execution_session_uncertain", "runner disconnected while reconciling the Execution Session", waitErr)
	}
	if errors.Is(waitErr, context.DeadlineExceeded) {
		_, err := s.transition(ctx, session, []string{"STARTING", "RUNNING"}, "FAILED", nil)
		return nil, err
	}
	_, transitionErr := s.transition(ctx, session, []string{"STARTING", "RUNNING"}, "FAILED", nil)
	return nil, errors.Join(waitErr, transitionErr)
}

func (s *ExecutionSessionService) failRunnerSessionAfterDisconnectTimeout(ctx context.Context, session store.ExecutionSession) error {
	if tracker, ok := s.registry.(runnerDisconnectTracker); ok {
		if disconnectedAt, known := tracker.DisconnectedSince(session.RunnerID); known && time.Since(disconnectedAt) >= runnerReconnectTimeout {
			_, err := s.transition(ctx, session, []string{"STARTING", "RUNNING"}, "FAILED", nil)
			return err
		}
	}
	return NewError("execution_session_uncertain", "runner is disconnected while Execution Session reconciliation is pending", runner.ErrDisconnected)
}

func (s *ExecutionSessionService) TerminateRunnerSessions(ctx context.Context, runnerID string) error {
	sessions, err := s.store.ListExecutionSessionsByRunner(ctx, runnerID, []string{"PENDING", "STARTING", "RUNNING"})
	if err != nil {
		return translateStoreError(err, "execution_session")
	}
	var errs []error
	for _, session := range sessions {
		if process, ok := s.liveProcess(session.ProjectID, session.ID); ok {
			if killErr := process.Kill(ctx); killErr != nil {
				errs = append(errs, killErr)
			}
		}
		if _, transitionErr := s.transition(ctx, session, []string{"PENDING", "STARTING", "RUNNING"}, "FAILED", nil); transitionErr != nil {
			errs = append(errs, transitionErr)
		}
	}
	return errors.Join(errs...)
}
