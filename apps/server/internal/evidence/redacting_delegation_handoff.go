package evidence

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *RedactingStore) MarkDelegationWorkspaceHandoffReady(ctx context.Context, projectID, parentRunID, delegationID, delegatedRunID string) error {
	base, ok := s.ControlPlaneStore.(store.DelegationWorkspaceHandoffStore)
	if !ok {
		return fmt.Errorf("redacting store base does not support delegation workspace handoff")
	}
	return base.MarkDelegationWorkspaceHandoffReady(ctx, projectID, parentRunID, delegationID, delegatedRunID)
}
