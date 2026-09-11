package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteRepositoryManagerUsesIndependentRepositoryLocks(t *testing.T) {
	manager := NewRemoteRepositoryManager(t.TempDir())
	firstKey := remoteRepositoryKey("https://example.invalid/acme/one.git")
	secondKey := remoteRepositoryKey("https://example.invalid/acme/two.git")

	first := manager.repositoryLock(firstKey)
	if again := manager.repositoryLock(firstKey); again != first {
		t.Fatal("same repository did not reuse its mutation lock")
	}
	if second := manager.repositoryLock(secondKey); second == first {
		t.Fatal("different repositories unexpectedly share a mutation lock")
	}
}

func TestRemoteRepositoryManagerRejectsCheckoutFromDifferentRepositoryCache(t *testing.T) {
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	checkout := RemoteCheckout{
		CachePath:     filepath.Join(root, "not-the-project-cache.git"),
		WorktreePath:  filepath.Join(root, "worktree"),
		Branch:        "agent-board/AB-55",
		StartRevision: strings.Repeat("a", 40),
	}

	if _, err := manager.Publish(context.Background(), "https://example.invalid/acme/project.git", checkout); err == nil || !strings.Contains(err.Error(), "does not belong to repository cache") {
		t.Fatalf("publish error=%v", err)
	}
}
