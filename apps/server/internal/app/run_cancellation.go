package app

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// CancelRun requests cancellation from the scheduler worker that currently
// owns the Run. The scheduler remains authoritative for the terminal Run
// transition and keeps its lease alive while execution cleanup finishes.
func (s *Services) CancelRun(ctx context.Context, projectID, runID string) error {
	if s == nil || s.ControlPlane == nil || s.Scheduler == nil {
		return NewError("run_cancellation_unavailable", "Run cancellation is unavailable", store.ErrConflict)
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(runID) == "" {
		return NewError("invalid_argument", "projectId and runId are required", store.ErrInvalidArgument)
	}
	run, err := s.ControlPlane.GetRun(ctx, projectID, runID)
	if err != nil {
		return err
	}
	switch run.Status {
	case "COMPLETED", "FAILED", "CANCELLED":
		return NewError("invalid_run_transition", "Run is already terminal", store.ErrConflict)
	}
	if !s.Scheduler.CancelRun(projectID, runID) {
		return NewError("run_cancellation_unavailable", "Run is not currently owned by this scheduler", store.ErrConflict)
	}
	return nil
}
