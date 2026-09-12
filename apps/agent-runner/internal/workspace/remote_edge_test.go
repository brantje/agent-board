package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteRepositoryManagerValidatesIdentityAndOwnership(t *testing.T) {
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	if manager.root != filepath.Join(root, ".agent-board", "git-cache") {
		t.Fatalf("cache root=%q", manager.root)
	}

	ctx := context.Background()
	if _, err := manager.Prepare(ctx, "", "", "agent-board/AB-1", "", filepath.Join(root, "worktree")); err == nil {
		t.Fatal("blank clone URL was accepted")
	}
	if _, err := manager.Prepare(ctx, "example", "", "feature/AB-1", "", filepath.Join(root, "worktree")); err == nil {
		t.Fatal("Issue branch outside agent-board namespace was accepted")
	}
	if _, err := manager.Prepare(ctx, "example", "", "agent-board/bad branch", "", filepath.Join(root, "worktree")); err == nil {
		t.Fatal("invalid Issue branch was accepted")
	}
	if _, err := manager.Publish(ctx, "", RemoteCheckout{}); err == nil {
		t.Fatal("incomplete publish identity was accepted")
	}
	if err := manager.Cleanup(ctx, "", RemoteCheckout{}); err == nil {
		t.Fatal("incomplete cleanup identity was accepted")
	}

	origin, _, _ := initRemoteRepository(t)
	checkout, err := manager.Prepare(ctx, origin, "", "agent-board/AB-1", "", filepath.Join(root, "session-1"))
	if err != nil {
		t.Fatal(err)
	}
	wrong := checkout
	wrong.CachePath = filepath.Join(root, "other.git")
	if _, err := manager.Publish(ctx, origin, wrong); err == nil {
		t.Fatal("publish accepted checkout from another cache")
	}
	if err := manager.Cleanup(ctx, origin, wrong); err == nil {
		t.Fatal("cleanup accepted checkout from another cache")
	}

	if _, err := manager.Prepare(ctx, origin, "", "agent-board/AB-2", "", checkout.WorktreePath); err == nil {
		t.Fatal("existing worktree path was accepted")
	}
}

func TestRemoteRepositoryManagerRejectsMissingRecordedAndUnpublishedLocalBranches(t *testing.T) {
	ctx := context.Background()
	origin, _, _ := initRemoteRepository(t)
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)

	unpublished, err := manager.Prepare(ctx, origin, "", "agent-board/AB-30", "", filepath.Join(root, "session-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Cleanup(ctx, origin, unpublished); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Prepare(ctx, origin, "", "agent-board/AB-30", "", filepath.Join(root, "session-2")); err == nil || !strings.Contains(err.Error(), "unpublished local Issue branch") {
		t.Fatalf("unpublished local branch was not rejected: %v", err)
	}

	published, err := manager.Prepare(ctx, origin, "", "agent-board/AB-31", "", filepath.Join(root, "session-3"))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := manager.Publish(ctx, origin, published)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Cleanup(ctx, origin, published); err != nil {
		t.Fatal(err)
	}
	runRemoteGit(t, origin, "update-ref", "-d", "refs/heads/agent-board/AB-31")
	if _, err := manager.Prepare(ctx, origin, "", "agent-board/AB-31", revision, filepath.Join(root, "session-4")); err == nil || !strings.Contains(err.Error(), "missing from origin") {
		t.Fatalf("missing recorded remote branch was not rejected: %v", err)
	}
}

func TestRemoteRepositoryManagerSourceRefFormsAndCacheValidation(t *testing.T) {
	ctx := context.Background()
	origin, working, mainRevision := initRemoteRepository(t)
	runRemoteGit(t, working, "tag", "v1")
	runRemoteGit(t, working, "push", "-q", "origin", "refs/tags/v1")

	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	checkout, err := manager.Prepare(ctx, origin, "refs/heads/main", "agent-board/AB-40", "", filepath.Join(root, "session-1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"refs/heads/main", "refs/remotes/origin/main", "refs/tags/v1", "v1"} {
		revision, err := resolveSourceRevision(ctx, checkout.CachePath, ref)
		if err != nil {
			t.Fatalf("resolve %q: %v", ref, err)
		}
		if revision != mainRevision {
			t.Fatalf("resolve %q=%q want=%q", ref, revision, mainRevision)
		}
	}
	if _, err := resolveSourceRevision(ctx, checkout.CachePath, "missing-ref"); err == nil {
		t.Fatal("missing configured source ref was accepted")
	}

	runRemoteGit(t, checkout.CachePath, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "different.git"))
	if _, err := manager.Prepare(ctx, origin, "main", "agent-board/AB-41", "", filepath.Join(root, "session-2")); err == nil || !strings.Contains(err.Error(), "origin does not match") {
		t.Fatalf("cache origin mismatch was not rejected: %v", err)
	}

	otherRoot := t.TempDir()
	other := NewRemoteRepositoryManager(otherRoot)
	cachePath := filepath.Join(other.root, remoteRepositoryKey(origin)+".git")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatal(err)
	}
	runRemoteGit(t, "", "init", "-q", cachePath)
	if _, err := other.Prepare(ctx, origin, "main", "agent-board/AB-42", "", filepath.Join(otherRoot, "session")); err == nil || !strings.Contains(err.Error(), "not a bare repository") {
		t.Fatalf("non-bare cache was not rejected: %v", err)
	}
}
