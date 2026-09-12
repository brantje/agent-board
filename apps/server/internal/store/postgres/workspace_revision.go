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
		SELECT id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, current_branch, bootstrap_status, created_at, updated_at
		FROM workspaces
		WHERE project_id = $1 AND id = $2
	`, projectID, workspaceID))
}

func (s *Store) GetWorkspaceCurrentRevision(ctx context.Context, projectID, workspaceID string) (string, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(workspaceID) == "" {
		return "", store.ErrInvalidArgument
	}
	var revision string
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(current_revision, '')
		FROM workspaces
		WHERE project_id=$1 AND id=$2
	`, projectID, workspaceID).Scan(&revision); err != nil {
		return "", notFound(err)
	}
	return strings.TrimSpace(revision), nil
}

func (s *Store) UpdateWorkspaceCurrentRevision(ctx context.Context, projectID, workspaceID, revision string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	workspaceID = strings.TrimSpace(workspaceID)
	revision = strings.TrimSpace(revision)
	if projectID == "" || workspaceID == "" || revision == "" {
		return "", store.ErrInvalidArgument
	}
	var persisted string
	if err := s.pool.QueryRow(ctx, `
		UPDATE workspaces
		SET current_revision=$3, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND bootstrap_status='READY'
		RETURNING current_revision
	`, projectID, workspaceID, revision).Scan(&persisted); err != nil {
		return "", notFound(err)
	}
	return persisted, nil
}

var _ store.WorkspaceRevisionStore = (*Store)(nil)
