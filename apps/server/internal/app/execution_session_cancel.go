package app

import (
	"context"
	"errors"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const defaultExecutionCancelGrace = 5 * time.Second

// Cancel targets one durable Execution Session. It first asks the runner to
// terminate that session's process tree, then escalates to kill after a bounded
// grace period. It never signals a later session merely because it reuses the
// same Runtime Instance.
func (s *ExecutionSessionService) Cancel(ctx context.Context, projectID, sessionID string, grace time.Duration) error {
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

	if err := process.Terminate(ctx); err != nil {
		select {
		case <-process.done:
			_, waitErr := process.Wait(ctx)
			return waitErr
		default:
		}
		return NewError("execution_session_cancel_uncertain", "could not terminate the Execution Session", err)
	}
	if grace <= 0 {
		grace = defaultExecutionCancelGrace
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
		if errors.Is(err, runner.ErrDisconnected) || errors.Is(err, runner.ErrClosed) {
			return NewError("execution_session_cancel_uncertain", "runner disconnected while force-killing the Execution Session", err)
		}
		return err
	}
	killWaitCtx, cancelKillWait := context.WithTimeout(ctx, grace)
	defer cancelKillWait()
	if _, err := process.Wait(killWaitCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return NewError("execution_session_cancel_uncertain", "the Execution Session did not exit after force-kill", err)
		}
		return err
	}
	return nil
}
