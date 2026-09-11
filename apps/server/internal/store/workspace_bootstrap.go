package store

import (
	"context"
	"errors"
)

var ErrWorkspaceBootstrapLockTimeout = errors.New("store: workspace bootstrap lock timeout")

// WorkspaceBootstrapLock serializes filesystem materialization for one durable
// Workspace across server processes. Release must be safe to call more than once.
type WorkspaceBootstrapLock interface {
	Release() error
}

// WorkspaceBootstrapStore owns the durable state transitions used by the
// filesystem materializer. It is intentionally separate from generic control-
// plane persistence so normal API test doubles do not need filesystem concerns.
type WorkspaceBootstrapStore interface {
	GetWorkspaceByIssue(ctx context.Context, projectID, issueID string) (Workspace, error)
	AcquireWorkspaceBootstrapLock(ctx context.Context, workspaceID string) (WorkspaceBootstrapLock, error)
	MarkWorkspaceBootstrapPending(ctx context.Context, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, baseRevision, workingBranch string) (Workspace, error)
	MarkWorkspaceBootstrapReady(ctx context.Context, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, baseRevision, workingBranch string) (Workspace, error)
	MarkWorkspaceBootstrapFailed(ctx context.Context, projectID, issueID, workspaceID string) (Workspace, error)
}

// WorkspaceExecutionLockStore lets the execution path enter the same filesystem
// critical section while proving it owns the Runner-backed Workspace lifecycle.
// Generic Workspace writers use WorkspaceBootstrapStore and are rejected while
// another Runner execution owns that Workspace.
type WorkspaceExecutionLockStore interface {
	AcquireWorkspaceExecutionLock(ctx context.Context, workspaceID, executionSessionID string) (WorkspaceBootstrapLock, error)
}
