package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Store) RegisterRunner(ctx context.Context, registrationHash []byte, v store.Runner) (store.Runner, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Runner{}, err
	}
	defer tx.Rollback(ctx)

	runner, err := scanRunner(tx.QueryRow(ctx, `
		UPDATE runners
		SET name=$2,
			token_hash=$3,
			registration_token_hash=NULL,
			registered_at=now(),
			updated_at=now()
		WHERE registration_token_hash=$1
			AND registered_at IS NULL
			AND NOT internal
			AND revoked_at IS NULL
			AND deleted_at IS NULL
		RETURNING `+runnerColumns, registrationHash, v.Name, v.TokenHash))
	if err != nil {
		return store.Runner{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Runner{}, err
	}
	return runner, nil
}
