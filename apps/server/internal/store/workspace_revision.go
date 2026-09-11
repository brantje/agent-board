package store

import "context"

// WorkspaceRevisionStore persists the last authoritative Issue branch commit.
// BaseRevision remains the immutable Issue baseline used for Review diffs.
type WorkspaceRevisionStore interface {
	GetWorkspaceCurrentRevision(context.Context, string, string) (string, error)
	UpdateWorkspaceCurrentRevision(context.Context, string, string, string) (string, error)
}
