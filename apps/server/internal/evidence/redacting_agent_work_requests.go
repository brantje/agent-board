package evidence

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *RedactingStore) GetAgentWorkRequestExecutionContext(ctx context.Context, projectID, runID string) (*store.AgentWorkRequestExecutionContext, error) {
	base, ok := s.ControlPlaneStore.(store.AgentWorkRequestExecutionContextStore)
	if !ok {
		return nil, fmt.Errorf("redacting store base does not support Agent work request execution context")
	}
	return base.GetAgentWorkRequestExecutionContext(ctx, projectID, runID)
}

func (s *RedactingStore) ReconcilePendingAgentWorkRequest(ctx context.Context) (store.AgentWorkRequestReconciliationResult, error) {
	base, ok := s.ControlPlaneStore.(store.AgentWorkRequestReconciliationStore)
	if !ok {
		return store.AgentWorkRequestReconciliationResult{}, fmt.Errorf("redacting store base does not support Agent work request reconciliation")
	}
	return base.ReconcilePendingAgentWorkRequest(ctx)
}

var _ store.AgentWorkRequestExecutionContextStore = (*RedactingStore)(nil)
var _ store.AgentWorkRequestReconciliationStore = (*RedactingStore)(nil)
