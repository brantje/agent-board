package postgres

import (
	"context"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Store) ListProjectRunnerIDs(ctx context.Context, projectID string) ([]string, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT runner_id::text FROM project_runners WHERE project_id=$1 ORDER BY runner_id`, projectID)
	if err != nil {
		return nil, notFound(err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) SetProjectRunnerIDs(ctx context.Context, projectID string, ids []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var project string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM projects WHERE id=$1 FOR UPDATE`, projectID).Scan(&project); err != nil {
		return notFound(err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM project_runners WHERE project_id=$1`, projectID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return store.ErrInvalidArgument
		}
		seen[id] = true
		tag, err := tx.Exec(ctx, `INSERT INTO project_runners(project_id,runner_id) SELECT $1,id FROM runners WHERE id=$2 AND NOT internal AND deleted_at IS NULL AND revoked_at IS NULL`, projectID, id)
		if err != nil {
			return notFound(err)
		}
		if tag.RowsAffected() != 1 {
			return store.ErrInvalidArgument
		}
	}
	return tx.Commit(ctx)
}
