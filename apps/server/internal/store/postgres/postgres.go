package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool             *pgxpool.Pool
	lockPool         *pgxpool.Pool
	runnerCandidates func(string) []string
}

// SetRunnerCandidates supplies live authenticated Engine-matching candidates;
// PostgreSQL still validates policy and reserves each selected runner atomically.
// Configure it once before starting scheduler workers.
func (s *Store) SetRunnerCandidates(candidates func(string) []string) {
	s.runnerCandidates = candidates
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	lockPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := lockPool.Ping(ctx); err != nil {
		lockPool.Close()
		pool.Close()
		return nil, err
	}
	return NewWithPools(pool, lockPool), nil
}

func New(pool *pgxpool.Pool) *Store {
	return NewWithPools(pool, pool)
}

func NewWithPools(pool, lockPool *pgxpool.Pool) *Store {
	return &Store{pool: pool, lockPool: lockPool}
}

func (s *Store) Close() {
	if s == nil {
		return
	}
	if s.lockPool != nil && s.lockPool != s.pool {
		s.lockPool.Close()
	}
	if s.pool != nil {
		s.pool.Close()
	}
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	return err
}

func objectJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return store.EmptyObject
	}
	return value
}

func arrayJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("[]")
	}
	return value
}

func commandArgvJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("[]")
	}
	return value
}

func translateConstraint(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503":
		return store.ErrNotFound
	case "23505":
		return store.ErrConflict
	case "23514", "23502", "22P02":
		return store.ErrInvalidArgument
	default:
		return err
	}
}
