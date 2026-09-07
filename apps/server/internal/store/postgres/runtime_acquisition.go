package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const runtimeAcquisitionLockPrefix = "agent-board:runtime-acquisition:"

// AcquireRuntimeAcquisitionLock serializes Runtime acquisition for a durable
// Workspace/Runtime pair across all server processes connected to PostgreSQL.
// The caller's context owns lock waiting; a crashed process releases the
// session advisory lock with its database connection.
func (s *Store) AcquireRuntimeAcquisitionLock(ctx context.Context, workspaceID, runtimeID string) (store.RuntimeAcquisitionLock, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	runtimeID = strings.TrimSpace(runtimeID)
	if workspaceID == "" || runtimeID == "" {
		return nil, store.ErrInvalidArgument
	}

	conn, err := s.lockPool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	key := runtimeAcquisitionLockPrefix + workspaceID + ":" + runtimeID
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, key); err != nil {
		discardPoolConn(conn)
		return nil, err
	}
	return &runtimeAcquisitionLock{conn: conn, key: key}, nil
}

type runtimeAcquisitionLock struct {
	once sync.Once
	conn *pgxpool.Conn
	key  string
	err  error
}

func (l *runtimeAcquisitionLock) Release() error {
	if l == nil || l.conn == nil {
		return nil
	}
	l.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		l.err = l.conn.QueryRow(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, l.key).Scan(&unlocked)
		if l.err == nil && !unlocked {
			l.err = errors.New("runtime acquisition advisory lock was not held")
		}
		if l.err != nil {
			discardPoolConn(l.conn)
			return
		}
		l.conn.Release()
	})
	return l.err
}
