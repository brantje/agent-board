package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const sourceConnectionSelectColumns = `id::text, project_id::text, kind, name, base_url, external_account_id, credential_ref, enabled, health_status, last_validated_at, created_at, updated_at`

func scanSourceConnection(row pgx.Row) (store.SourceConnection, error) {
	var value store.SourceConnection
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Kind, &value.Name, &value.BaseURL, &value.ExternalAccountID, &value.CredentialRef, &value.Enabled, &value.HealthStatus, &value.LastValidatedAt, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.SourceConnection{}, notFound(err)
	}
	return value, nil
}

func (s *Store) ListSourceConnections(ctx context.Context, projectID *string) ([]store.SourceConnection, error) {
	project, scoped := visibleScope(projectID)
	rows, err := s.pool.Query(ctx, `SELECT `+sourceConnectionSelectColumns+` FROM source_connections
		WHERE ($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL)
		ORDER BY project_id NULLS FIRST, created_at, id`, nullableUUID(project, scoped), scoped)
	if err != nil { return nil, err }
	defer rows.Close()
	out := make([]store.SourceConnection, 0)
	for rows.Next() {
		value, err := scanSourceConnection(rows)
		if err != nil { return nil, err }
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) GetSourceConnection(ctx context.Context, projectID *string, connectionID string) (store.SourceConnection, error) {
	project, scoped := visibleScope(projectID)
	return scanSourceConnection(s.pool.QueryRow(ctx, `SELECT `+sourceConnectionSelectColumns+` FROM source_connections
		WHERE id=$3 AND (($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL))`,
		nullableUUID(project, scoped), scoped, connectionID))
}

func (s *Store) CreateSourceConnection(ctx context.Context, input store.SourceConnection) (store.SourceConnection, error) {
	health := input.HealthStatus
	if health == "" { health = "UNKNOWN" }
	return scanSourceConnection(s.pool.QueryRow(ctx, `INSERT INTO source_connections
		(project_id, kind, name, base_url, external_account_id, credential_ref, enabled, health_status, last_validated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+sourceConnectionSelectColumns,
		input.ProjectID, input.Kind, input.Name, input.BaseURL, input.ExternalAccountID, input.CredentialRef, input.Enabled, health, input.LastValidatedAt))
}

func (s *Store) UpdateSourceConnection(ctx context.Context, projectID *string, input store.SourceConnection) (store.SourceConnection, error) {
	return scanSourceConnection(s.pool.QueryRow(ctx, `UPDATE source_connections
		SET kind=$3, name=$4, base_url=$5, external_account_id=$6, credential_ref=$7, enabled=$8, health_status=$9, last_validated_at=$10, updated_at=now()
		WHERE id=$2 AND project_id IS NOT DISTINCT FROM $1::uuid RETURNING `+sourceConnectionSelectColumns,
		projectID, input.ID, input.Kind, input.Name, input.BaseURL, input.ExternalAccountID, input.CredentialRef, input.Enabled, input.HealthStatus, input.LastValidatedAt))
}
