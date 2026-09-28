package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const sourceRepositorySelectColumns = `id::text, source_connection_id::text, external_id, namespace, name, path, web_url, clone_url, ssh_clone_url, default_branch, archived, disabled, last_synced_at, created_at, updated_at`
const sourceRepositoryJoinedSelectColumns = `r.id::text, r.source_connection_id::text, r.external_id, r.namespace, r.name, r.path, r.web_url, r.clone_url, r.ssh_clone_url, r.default_branch, r.archived, r.disabled, r.last_synced_at, r.created_at, r.updated_at`

func scanSourceRepository(row pgx.Row) (store.SourceRepository, error) {
	var value store.SourceRepository
	if err := row.Scan(&value.ID, &value.SourceConnectionID, &value.ExternalID, &value.Namespace, &value.Name, &value.Path, &value.WebURL, &value.CloneURL, &value.SSHCloneURL, &value.DefaultBranch, &value.Archived, &value.Disabled, &value.LastSyncedAt, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.SourceRepository{}, notFound(err)
	}
	return value, nil
}

func (s *Store) ListSourceRepositories(ctx context.Context, projectID *string, connectionID string) ([]store.SourceRepository, error) {
	project, scoped := visibleScope(projectID)
	rows, err := s.pool.Query(ctx, `SELECT `+sourceRepositoryJoinedSelectColumns+` FROM source_repositories AS r
		JOIN source_connections AS c ON c.id=r.source_connection_id
		WHERE r.source_connection_id=$3 AND (($2::boolean AND (c.project_id IS NULL OR c.project_id=$1::uuid)) OR (NOT $2::boolean AND c.project_id IS NULL))
		ORDER BY r.created_at, r.id`, nullableUUID(project, scoped), scoped, connectionID)
	if err != nil { return nil, err }
	defer rows.Close()
	out := make([]store.SourceRepository, 0)
	for rows.Next() {
		value, err := scanSourceRepository(rows)
		if err != nil { return nil, err }
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) GetSourceRepository(ctx context.Context, projectID *string, connectionID, repositoryID string) (store.SourceRepository, error) {
	project, scoped := visibleScope(projectID)
	return scanSourceRepository(s.pool.QueryRow(ctx, `SELECT `+sourceRepositoryJoinedSelectColumns+` FROM source_repositories AS r
		JOIN source_connections AS c ON c.id=r.source_connection_id
		WHERE r.source_connection_id=$3 AND r.id=$4 AND (($2::boolean AND (c.project_id IS NULL OR c.project_id=$1::uuid)) OR (NOT $2::boolean AND c.project_id IS NULL))`,
		nullableUUID(project, scoped), scoped, connectionID, repositoryID))
}

func (s *Store) UpsertSourceRepository(ctx context.Context, input store.SourceRepository) (store.SourceRepository, error) {
	return scanSourceRepository(s.pool.QueryRow(ctx, `INSERT INTO source_repositories
		(source_connection_id, external_id, namespace, name, path, web_url, clone_url, ssh_clone_url, default_branch, archived, disabled, last_synced_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (source_connection_id, external_id) DO UPDATE SET
			namespace=EXCLUDED.namespace, name=EXCLUDED.name, path=EXCLUDED.path, web_url=EXCLUDED.web_url,
			clone_url=EXCLUDED.clone_url, ssh_clone_url=EXCLUDED.ssh_clone_url, default_branch=EXCLUDED.default_branch,
			archived=EXCLUDED.archived, disabled=EXCLUDED.disabled, last_synced_at=EXCLUDED.last_synced_at, updated_at=now()
		RETURNING `+sourceRepositorySelectColumns,
		input.SourceConnectionID, input.ExternalID, input.Namespace, input.Name, input.Path, input.WebURL,
		input.CloneURL, input.SSHCloneURL, input.DefaultBranch, input.Archived, input.Disabled, input.LastSyncedAt))
}
