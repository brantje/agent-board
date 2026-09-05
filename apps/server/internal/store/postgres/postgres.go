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
	pool     *pgxpool.Pool
	lockPool *pgxpool.Pool
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
	if lockPool == nil {
		lockPool = pool
	}
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
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return store.ErrConflict
		case "23502", "23503", "23514", "22P02":
			return store.ErrInvalidArgument
		}
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
		return json.RawMessage(`[]`)
	}
	return value
}
