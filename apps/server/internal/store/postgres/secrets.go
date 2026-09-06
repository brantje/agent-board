package postgres

import (
	"context"
	"errors"

	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/jackc/pgx/v5"
)

func (s *Store) PutSecret(ctx context.Context, input secrets.Record) (secrets.Record, error) {
	if input.ProjectID == nil {
		return scanSecret(s.pool.QueryRow(ctx, `
			INSERT INTO secrets (project_id, ref, ciphertext, key_version)
			VALUES (NULL, $1, $2, $3)
			ON CONFLICT (ref) WHERE project_id IS NULL
			DO UPDATE SET ciphertext = EXCLUDED.ciphertext, key_version = EXCLUDED.key_version, updated_at = now()
			RETURNING id::text, project_id::text, ref, ciphertext, key_version
		`, input.Ref, input.Ciphertext, input.KeyVersion))
	}
	return scanSecret(s.pool.QueryRow(ctx, `
		INSERT INTO secrets (project_id, ref, ciphertext, key_version)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, ref) WHERE project_id IS NOT NULL
		DO UPDATE SET ciphertext = EXCLUDED.ciphertext, key_version = EXCLUDED.key_version, updated_at = now()
		RETURNING id::text, project_id::text, ref, ciphertext, key_version
	`, input.ProjectID, input.Ref, input.Ciphertext, input.KeyVersion))
}

func (s *Store) GetSecret(ctx context.Context, projectID *string, ref string) (secrets.Record, error) {
	if projectID == nil {
		return scanSecret(s.pool.QueryRow(ctx, `
			SELECT id::text, project_id::text, ref, ciphertext, key_version
			FROM secrets
			WHERE project_id IS NULL AND ref = $1
		`, ref))
	}
	return scanSecret(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, ref, ciphertext, key_version
		FROM secrets
		WHERE project_id = $1 AND ref = $2
	`, projectID, ref))
}

func scanSecret(row pgx.Row) (secrets.Record, error) {
	var value secrets.Record
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Ref, &value.Ciphertext, &value.KeyVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return secrets.Record{}, secrets.ErrNotFound
		}
		return secrets.Record{}, err
	}
	return value, nil
}
