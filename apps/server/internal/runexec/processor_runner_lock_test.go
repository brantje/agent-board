package runexec

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *runnerSyncStore) AcquireWorkspaceExecutionLock(ctx context.Context, workspaceID, _ string) (store.WorkspaceBootstrapLock, error) {
	return s.AcquireWorkspaceBootstrapLock(ctx, workspaceID)
}
