package app

import (
	"context"
	"errors"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	defaultExecutionCancelGrace    = 5 * time.Second
	defaultExecutionCancelKillWait = 5 * time.Second
)

// Cancel targets one durable Execution Session. It first asks the runner to
// terminate that session's process tree, then escalates to kill after a bounded
// grace period. It never signals a later session merely because it reuses the
// same Runtime Instance.
func (s *ExecutionSessionService) Cancel(ctx context.Context, projectID, sessionID string, grace time.Duration) error {
	return s.cancel(ctx, projectID, sessionID, grace, defaultExecutionCancelKillWait)
}

func (s *ExecutionSessionService) cancel(ctx context.Context, projectID, sessionID string, grace, killWait time.Duration) error {
	if projectID == "" || sessionID == "" {
		return NewError("invalid_argument", "projectId and executionSessionId are required", store.ErrInvalidArgument)
	}
	process, ok := s.liveProcess(projectID, sessionID)
	if !ok {
		var err error
		process, err = s.Reconcile(ctx, projectID, sessionID)
		if err != nil {
			return err
		}
		if process == nil {
			return nil
		}
	}

	if grace <= 0 {
		grace = defaultExecutionCancelGrace
	}
	if killWait <= 0 {
		killWait = defaultExecutionCancelKillWait
	}

	terminateErr := process.Terminate(ctx)
	if terminateErr != nil {
		select {
		case <-process.done:
			_, waitErr := process.Wait(ctx)
			return waitErr
		default:
		}
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-process.done:
		_, err := process.Wait(ctx)
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}

	if err := process.Kill(ctx); err != nil {
		select {
		case <-process.done:
			_, waitErr := process.Wait(ctx)
			return waitErr
		default:
		}
		cause := err
		if terminateErr != nil {
			cause = errors.Join(terminateErr, err)
		}
		message := "could not force-kill the Execution Session"
		if errors.Is(err, runner.ErrDisconnected) || errors.Is(err, runner.ErrClosed) {
			message = "runner disconnected while force-killing the Execution Session"
		}
		return NewError("execution_session_cancel_uncertain", message, cause)
	}

	killWaitCtx, cancelKillWait := context.WithTimeout(ctx, killWait)
	defer cancelKillWait()
	if _, err := process.Wait(killWaitCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return NewError("execution_session_cancel_uncertain", "the Execution Session did not exit after force-kill", err)
		}
		return err
	}
	return nil
}
