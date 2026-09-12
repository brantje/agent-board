package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Cleanup removes a successfully published remote Issue worktree. It never
// forces removal: a checkout that is no longer clean is retained for recovery.
func (m *RemoteRepositoryManager) Cleanup(ctx context.Context, cloneURL string, checkout RemoteCheckout) error {
	cloneURL = strings.TrimSpace(cloneURL)
	if cloneURL == "" || strings.TrimSpace(checkout.CachePath) == "" || strings.TrimSpace(checkout.WorktreePath) == "" {
		return fmt.Errorf("complete remote checkout identity is required")
	}
	key := remoteRepositoryKey(cloneURL)
	expectedCache := filepath.Join(m.root, key+".git")
	if filepath.Clean(checkout.CachePath) != expectedCache {
		return fmt.Errorf("remote checkout does not belong to repository cache")
	}
	lock := m.repositoryLock(key)
	lock.Lock()
	defer lock.Unlock()
	if _, err := runGit(ctx, "-C", checkout.CachePath, "worktree", "remove", checkout.WorktreePath); err != nil {
		return fmt.Errorf("remove clean remote Issue worktree: %w", err)
	}
	return nil
}
