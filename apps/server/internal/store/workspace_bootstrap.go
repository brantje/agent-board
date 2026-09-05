package store

import "context"

// WorkspaceBootstrapLock serializes filesystem materialization for one durable
// Workspace across server processes. Release must be safe to call more than once.
type WorkspaceBootstrapLock interface {
	Release() error
}

// WorkspaceBootstrapStore owns the durable state transitions used by the
// filesystem materializer. It is intentionally separate from generic control-
// plane persistence so normal API test doubles do not need filesystem concerns.
type WorkspaceBootstrapStore interface {
	GetWorkspaceByIssue(context.Context, string, string) (Workspace, error)
	AcquireWorkspaceBootstrapLock(context.Context, string) (WorkspaceBootstrapLock, error)
	MarkWorkspaceBootstrapPending(context.Context, string, string, string, string, string, string, string) (Workspace, error)
	MarkWorkspaceBootstrapReady(context.Context, string, string, string, string, string, string, string, string) (Workspace, error)
	MarkWorkspaceBootstrapFailed(context.Context, string, string, string) (Workspace, error)
}
