package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *runEvidenceTestStore) ListExecutionSessionsByRun(_ context.Context, projectID, runID string, _ []string) ([]store.ExecutionSession, error) {
	if projectID != s.run.ProjectID || runID != s.run.ID {
		return nil, store.ErrNotFound
	}
	sessions := make([]store.ExecutionSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		if session.ProjectID == projectID && session.RunID == runID {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}
