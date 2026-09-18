package app

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// CancelRun uses the scheduler cancellation boundary for actively owned Runs
// and a durable store transition only for Runs that are provably inactive.
// Any unfinished delegated child is cancelled through this same boundary.
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

	cancelled := s.Scheduler.CancelRun(projectID, runID)
	if !cancelled {
		inactive, ok := s.ControlPlane.store.(store.InactiveRunCancellationStore)
		if !ok {
			return NewError("run_cancellation_unavailable", "Run is not currently owned by this scheduler", store.ErrConflict)
		}
		result, cancelErr := inactive.CancelInactiveRun(ctx, projectID, runID)
		if cancelErr != nil {
			if errors.Is(cancelErr, store.ErrConflict) {
				return NewError("run_cancellation_unavailable", "Run is not safely inactive and is not currently owned by this scheduler", cancelErr)
			}
			return cancelErr
		}
		cancelled = true
		events := result.Events
		if len(events) == 0 && result.Event.ID != "" {
			events = []store.Event{result.Event}
		}
		publishPersistedEvents(ctx, s.Events, events)
	}
	if cancelled {
		if err := s.cancelDelegatedChildren(ctx, projectID, runID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Services) cancelDelegatedChildren(ctx context.Context, projectID, parentRunID string) error {
	delegations, ok := s.ControlPlane.store.(store.DelegationStore)
	if !ok {
		return nil
	}
	values, err := delegations.ListDelegationsByParentRun(ctx, projectID, parentRunID)
	if err != nil {
		return err
	}
	for _, delegation := range values {
		if delegation.Outcome != nil {
			continue
		}
		child, err := s.ControlPlane.GetRun(ctx, projectID, delegation.DelegatedRunID)
		if err != nil {
			return err
		}
		switch child.Status {
		case "COMPLETED", "FAILED", "CANCELLED":
			continue
		}
		if err := s.CancelRun(ctx, projectID, child.ID); err != nil {
			return err
		}
	}
	return nil
}
