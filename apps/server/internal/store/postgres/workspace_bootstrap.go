package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	workspaceBootstrapLockPrefix      = "agent-board:workspace-bootstrap:"
	workspaceBootstrapLockWaitTimeout = 5 * time.Second
)

func (s *Store) AcquireWorkspaceBootstrapLock(ctx context.Context, workspaceID string) (store.WorkspaceBootstrapLock, error) {
	return s.acquireWorkspaceBootstrapLock(ctx, workspaceID, workspaceBootstrapLockWaitTimeout)
}

func (s *Store) acquireWorkspaceBootstrapLock(ctx context.Context, workspaceID string, waitTimeout time.Duration) (store.WorkspaceBootstrapLock, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, store.ErrInvalidArgument
	}
	lockCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()

	conn, err := s.pool.Acquire(lockCtx)
	if err != nil {
		return nil, workspaceBootstrapLockWaitError(ctx, lockCtx, workspaceID, err)
	}
	key := workspaceBootstrapLockPrefix + workspaceID
	if _, err := conn.Exec(lockCtx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, key); err != nil {
		discardPoolConn(conn)
		return nil, workspaceBootstrapLockWaitError(ctx, lockCtx, workspaceID, err)
	}
	return &workspaceBootstrapLock{conn: conn, key: key}, nil
}

func workspaceBootstrapLockWaitError(parent, lockCtx context.Context, workspaceID string, err error) error {
	if parent.Err() == nil && errors.Is(lockCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("workspace %s: %w", workspaceID, store.ErrWorkspaceBootstrapLockTimeout)
	}
	return err
}

type workspaceBootstrapLock struct {
	once sync.Once
	conn *pgxpool.Conn
	key  string
	err  error
}

func (l *workspaceBootstrapLock) Release() error {
	if l == nil || l.conn == nil {
		return nil
	}
	l.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		l.err = l.conn.QueryRow(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, l.key).Scan(&unlocked)
		if l.err == nil && !unlocked {
			l.err = errors.New("workspace bootstrap advisory lock was not held")
		}
		if l.err != nil {
			discardPoolConn(l.conn)
			return
		}
		l.conn.Release()
	})
	return l.err
}

func discardPoolConn(conn *pgxpool.Conn) {
	if conn == nil {
		return
	}
	raw := conn.Hijack()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = raw.Close(ctx)
}

func (s *Store) MarkWorkspaceBootstrapPending(ctx context.Context, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, workingBranch string) (store.Workspace, error) {
	return s.workspaceBootstrapTransition(ctx, projectID, issueID, workspaceID, s.pool.QueryRow(ctx, `
		UPDATE workspaces
		SET path=$4,
		    repository_path=$5,
		    base_branch=$6,
		    base_revision=NULL,
		    working_branch=$7,
		    bootstrap_status='PENDING',
		    updated_at=now()
		WHERE project_id=$1 AND issue_id=$2 AND id=$3 AND bootstrap_status <> 'READY'
		RETURNING id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, bootstrap_status, created_at, updated_at
	`, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, workingBranch))
}

func (s *Store) MarkWorkspaceBootstrapReady(ctx context.Context, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, baseRevision, workingBranch string) (store.Workspace, error) {
	if strings.TrimSpace(baseRevision) == "" {
		return store.Workspace{}, store.ErrInvalidArgument
	}
	return s.workspaceBootstrapTransition(ctx, projectID, issueID, workspaceID, s.pool.QueryRow(ctx, `
		UPDATE workspaces
		SET path=$4,
		    repository_path=$5,
		    base_branch=$6,
		    base_revision=$7,
		    working_branch=$8,
		    bootstrap_status='READY',
		    updated_at=now()
		WHERE project_id=$1 AND issue_id=$2 AND id=$3 AND bootstrap_status IN ('PENDING', 'FAILED')
		RETURNING id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, bootstrap_status, created_at, updated_at
	`, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, baseRevision, workingBranch))
}

func (s *Store) MarkWorkspaceBootstrapFailed(ctx context.Context, projectID, issueID, workspaceID string) (store.Workspace, error) {
	return s.workspaceBootstrapTransition(ctx, projectID, issueID, workspaceID, s.pool.QueryRow(ctx, `
		UPDATE workspaces
		SET bootstrap_status='FAILED', updated_at=now()
		WHERE project_id=$1 AND issue_id=$2 AND id=$3 AND bootstrap_status <> 'READY'
		RETURNING id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, bootstrap_status, created_at, updated_at
	`, projectID, issueID, workspaceID))
}

func (s *Store) workspaceBootstrapTransition(ctx context.Context, projectID, issueID, workspaceID string, row interface {
	Scan(...any) error
}) (store.Workspace, error) {
	value, err := scanWorkspace(row)
	if !errors.Is(err, store.ErrNotFound) {
		return value, err
	}
	existing, getErr := s.GetWorkspaceByIssue(ctx, projectID, issueID)
	if getErr == nil && existing.ID == workspaceID && existing.BootstrapStatus == "READY" {
		return store.Workspace{}, store.ErrConflict
	}
	if getErr != nil && !errors.Is(getErr, store.ErrNotFound) {
		return store.Workspace{}, fmt.Errorf("reload workspace after transition conflict: %w", getErr)
	}
	return store.Workspace{}, store.ErrNotFound
}
