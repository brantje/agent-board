package postgres

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const runnerColumns = `id::text, COALESCE(name,''), token_hash, registration_token_hash, internal, registered_at, revoked_at, deleted_at, last_seen_at, capabilities, created_at, updated_at`

func scanRunner(row pgx.Row) (store.Runner, error) {
	var v store.Runner
	err := row.Scan(&v.ID, &v.Name, &v.TokenHash, &v.RegistrationTokenHash, &v.Internal, &v.RegisteredAt, &v.RevokedAt, &v.DeletedAt, &v.LastSeenAt, &v.Capabilities, &v.CreatedAt, &v.UpdatedAt)
	return v, notFound(err)
}

func (s *Store) CreateRunner(ctx context.Context, v store.Runner) (store.Runner, error) {
	return scanRunner(s.pool.QueryRow(ctx, `
		INSERT INTO runners (name,token_hash,registration_token_hash,internal,registered_at)
		VALUES (NULLIF($1,''),$2,$3,$4,CASE WHEN $2::bytea IS NULL THEN NULL ELSE now() END)
		RETURNING `+runnerColumns, v.Name, v.TokenHash, v.RegistrationTokenHash, v.Internal))
}

func (s *Store) GetRunner(ctx context.Context, id string) (store.Runner, error) {
	return scanRunner(s.pool.QueryRow(ctx, `SELECT `+runnerColumns+` FROM runners WHERE id=$1 AND deleted_at IS NULL`, id))
}

func (s *Store) ListRunners(ctx context.Context) ([]store.Runner, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+runnerColumns+` FROM runners WHERE deleted_at IS NULL ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []store.Runner{}
	for rows.Next() {
		v, err := scanRunner(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func (s *Store) RenameRunner(ctx context.Context, id, name string) (store.Runner, error) {
	return scanRunner(s.pool.QueryRow(ctx, `UPDATE runners SET name=$2,updated_at=now() WHERE id=$1 AND deleted_at IS NULL AND registered_at IS NOT NULL AND NOT internal RETURNING `+runnerColumns, id, name))
}

func (s *Store) RotateRunnerCredential(ctx context.Context, id string, hash []byte) (store.Runner, error) {
	return scanRunner(s.pool.QueryRow(ctx, `UPDATE runners SET token_hash=$2,updated_at=now() WHERE id=$1 AND deleted_at IS NULL AND revoked_at IS NULL AND registered_at IS NOT NULL RETURNING `+runnerColumns, id, hash))
}

func (s *Store) RevokeRunner(ctx context.Context, id string, deleted bool) (store.Runner, error) {
	return scanRunner(s.pool.QueryRow(ctx, `
		UPDATE runners
		SET revoked_at=COALESCE(revoked_at,now()),
			registration_token_hash=NULL,
			deleted_at=CASE WHEN $2 THEN now() ELSE deleted_at END,
			updated_at=now()
		WHERE id=$1 AND deleted_at IS NULL AND NOT internal
		RETURNING `+runnerColumns, id, deleted))
}

func (s *Store) ObserveRunner(ctx context.Context, id string, capabilities json.RawMessage) error {
	tag, err := s.pool.Exec(ctx, `UPDATE runners SET last_seen_at=now(),capabilities=$2,updated_at=now() WHERE id=$1 AND deleted_at IS NULL AND revoked_at IS NULL AND registered_at IS NOT NULL`, id, capabilities)
	if err != nil {
		return notFound(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) CountRunnerReservations(ctx context.Context, ids []string) (map[string]int, error) {
	counts := map[string]int{}
	if len(ids) == 0 {
		return counts, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT resource_id::text, count(*)
		FROM scheduler_capacity_reservations
		WHERE resource_kind='RUNNER' AND resource_id = ANY($1::uuid[])
		GROUP BY resource_id
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		counts[id] = count
	}
	return counts, rows.Err()
}
