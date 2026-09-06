package httpapi

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *httpRunEvidenceStore) ListExecutionSessionsByRun(ctx context.Context, pid, requestedRunID string, statuses []string) ([]store.ExecutionSession, error) {
	if pid != projectID || requestedRunID != runID {
		return nil, store.ErrNotFound
	}
	return s.ListExecutionSessions(ctx, pid, statuses)
}
