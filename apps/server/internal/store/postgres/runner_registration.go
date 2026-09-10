package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Store) CreateRunnerRegistration(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO runner_registrations (token_hash) VALUES ($1)`, tokenHash)
	return notFound(err)
}

func (s *Store) RegisterRunner(ctx context.Context, registrationHash []byte, v store.Runner) (store.Runner, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Runner{}, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `DELETE FROM runner_registrations WHERE token_hash=$1`, registrationHash)
	if err != nil {
		return store.Runner{}, notFound(err)
	}
	if tag.RowsAffected() != 1 {
		return store.Runner{}, store.ErrNotFound
	}

	runner, err := scanRunner(tx.QueryRow(ctx, `INSERT INTO runners (name,token_hash,internal) VALUES ($1,$2,false) RETURNING `+runnerColumns, v.Name, v.TokenHash))
	if err != nil {
		return store.Runner{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Runner{}, err
	}
	return runner, nil
}
