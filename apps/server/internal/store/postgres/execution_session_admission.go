package postgres

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Store) GetOrCreateExecutionSessionAdmissionPrompt(ctx context.Context, projectID, sessionID, prompt string) (string, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(prompt) == "" {
		return "", store.ErrInvalidArgument
	}
	var admitted string
	if err := s.pool.QueryRow(ctx, `
		UPDATE execution_sessions
		SET admission_prompt = COALESCE(admission_prompt, $3)
		WHERE project_id = $1 AND id = $2
		RETURNING admission_prompt
	`, projectID, sessionID, prompt).Scan(&admitted); err != nil {
		return "", notFound(err)
	}
	return admitted, nil
}
