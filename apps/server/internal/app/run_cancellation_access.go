package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// CancelRunForUser puts normal Project workflow authorization in front of the
// canonical cancellation path. Services.CancelRun remains the lifecycle
// authority and continues to delegate the terminal transition to the scheduler.
func (s *Services) CancelRunForUser(ctx context.Context, actor AuthenticatedUser, projectID, runID string) error {
	if s == nil || s.ProjectAccess == nil {
		return NewError("run_cancellation_unavailable", "Run cancellation is unavailable", store.ErrConflict)
	}
	if err := s.ProjectAccess.AuthorizeWorkflowMutation(ctx, actor, projectID); err != nil {
		return err
	}
	return s.CancelRun(ctx, projectID, runID)
}
