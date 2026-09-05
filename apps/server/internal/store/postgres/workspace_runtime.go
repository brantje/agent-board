package postgres

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Store) GetWorkspace(ctx context.Context, projectID, workspaceID string) (store.Workspace, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(workspaceID) == "" {
		return store.Workspace{}, store.ErrInvalidArgument
	}
	return scanWorkspace(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, bootstrap_status, created_at, updated_at
		FROM workspaces
		WHERE project_id = $1 AND id = $2
	`, projectID, workspaceID))
}
