package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteRepositoryManagerMutatesDifferentRepositoryCachesConcurrently(t *testing.T) {
	firstOrigin, _, _ := initRemoteRepository(t)
	secondOrigin, _, _ := initRemoteRepository(t)
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)

	// Hold the first repository's mutation lock so its real Prepare operation
	// cannot complete. A different repository Prepare must still finish while
	// that lock is held, proving mutations are not globally serialized.
	firstLock := manager.repositoryLock(remoteRepositoryKey(firstOrigin))
	firstLock.Lock()
	firstDone := make(chan error, 1)
	go func() {
		_, err := manager.Prepare(context.Background(), firstOrigin, "main", "agent-board/AB-60", "", filepath.Join(root, "first-session"))
		firstDone <- err
	}()

	secondDone := make(chan error, 1)
	go func() {
		_, err := manager.Prepare(context.Background(), secondOrigin, "main", "agent-board/AB-61", "", filepath.Join(root, "second-session"))
		secondDone <- err
	}()

	select {
	case err := <-secondDone:
		if err != nil {
			firstLock.Unlock()
			t.Fatalf("different-repository Prepare() error=%v", err)
		}
	case <-time.After(3 * time.Second):
		firstLock.Unlock()
		<-firstDone
		t.Fatal("different repository mutation was blocked by another repository cache")
	}

	firstLock.Unlock()
	if err := <-firstDone; err != nil {
		t.Fatalf("first Prepare() error=%v", err)
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
