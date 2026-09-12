package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRemoteRepositoryManagerUsesBareCacheAndSeparatedRefs(t *testing.T) {
	origin, _, mainRevision := initRemoteRepository(t)
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	checkout, err := manager.Prepare(context.Background(), origin, "", "agent-board/AB-1", "", filepath.Join(root, "session-1"))
	if err != nil {
		t.Fatal(err)
	}
	if checkout.StartRevision != mainRevision {
		t.Fatalf("start revision=%q want=%q", checkout.StartRevision, mainRevision)
	}
	if got := remoteGitOutput(t, checkout.CachePath, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Fatalf("cache bare=%q", got)
	}
	if _, err := remoteGitOutputError(checkout.CachePath, "show-ref", "--verify", "refs/remotes/origin/main"); err != nil {
		t.Fatalf("missing remote-tracking main: %v", err)
	}
	if _, err := remoteGitOutputError(checkout.CachePath, "show-ref", "--verify", "refs/heads/main"); err == nil {
		t.Fatal("remote source branch leaked into refs/heads")
	}
	if _, err := remoteGitOutputError(checkout.CachePath, "show-ref", "--verify", "refs/heads/agent-board/AB-1"); err != nil {
		t.Fatalf("missing local Issue branch: %v", err)
	}
}

func TestRemoteRepositoryManagerFetchesLaterChangesAndConfiguredRef(t *testing.T) {
	origin, working, _ := initRemoteRepository(t)
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	first, err := manager.Prepare(context.Background(), origin, "main", "agent-board/AB-1", "", filepath.Join(root, "session-1"))
	if err != nil {
		t.Fatal(err)
	}

	runRemoteGit(t, working, "checkout", "-qb", "feature")
	if err := os.WriteFile(filepath.Join(working, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRemoteGit(t, working, "add", "feature.txt")
	runRemoteGit(t, working, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "feature")
	featureRevision := remoteGitOutput(t, working, "rev-parse", "HEAD")
	runRemoteGit(t, working, "push", "-q", "origin", "feature")

	second, err := manager.Prepare(context.Background(), origin, "feature", "agent-board/AB-2", "", filepath.Join(root, "session-2"))
	if err != nil {
		t.Fatal(err)
	}
	if second.CachePath != first.CachePath {
		t.Fatalf("same repository used different caches: %q != %q", second.CachePath, first.CachePath)
	}
	if second.StartRevision != featureRevision {
		t.Fatalf("configured ref start=%q want=%q", second.StartRevision, featureRevision)
	}
}

func TestRemoteRepositoryManagerRefreshesOriginHead(t *testing.T) {
	origin, working, _ := initRemoteRepository(t)
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	if _, err := manager.Prepare(context.Background(), origin, "", "agent-board/AB-1", "", filepath.Join(root, "session-1")); err != nil {
		t.Fatal(err)
	}

	runRemoteGit(t, working, "checkout", "-qb", "trunk")
	if err := os.WriteFile(filepath.Join(working, "trunk.txt"), []byte("trunk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRemoteGit(t, working, "add", "trunk.txt")
	runRemoteGit(t, working, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "trunk")
	trunkRevision := remoteGitOutput(t, working, "rev-parse", "HEAD")
	runRemoteGit(t, working, "push", "-q", "origin", "trunk")
	runRemoteGit(t, origin, "symbolic-ref", "HEAD", "refs/heads/trunk")

	checkout, err := manager.Prepare(context.Background(), origin, "", "agent-board/AB-2", "", filepath.Join(root, "session-2"))
	if err != nil {
		t.Fatal(err)
	}
	if checkout.StartRevision != trunkRevision {
		t.Fatalf("default branch start=%q want=%q", checkout.StartRevision, trunkRevision)
	}
	if got := remoteGitOutput(t, checkout.CachePath, "symbolic-ref", "refs/remotes/origin/HEAD"); got != "refs/remotes/origin/trunk" {
		t.Fatalf("origin/HEAD=%q", got)
	}
}

func TestRemoteRepositoryManagerPublishesAndAnotherRunnerContinues(t *testing.T) {
	origin, _, _ := initRemoteRepository(t)
	firstRoot := t.TempDir()
	firstManager := NewRemoteRepositoryManager(firstRoot)
	first, err := firstManager.Prepare(context.Background(), origin, "", "agent-board/AB-9", "", filepath.Join(firstRoot, "session-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.WorktreePath, "runner.txt"), []byte("first runner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	published, err := firstManager.Publish(context.Background(), origin, first)
	if err != nil {
		t.Fatal(err)
	}
	if got := remoteGitOutput(t, origin, "rev-parse", "refs/heads/agent-board/AB-9"); got != published {
		t.Fatalf("published remote head=%q want=%q", got, published)
	}
	if err := firstManager.Cleanup(context.Background(), origin, first); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("published worktree was not removed: %v", err)
	}

	secondRoot := t.TempDir()
	secondManager := NewRemoteRepositoryManager(secondRoot)
	second, err := secondManager.Prepare(context.Background(), origin, "", "agent-board/AB-9", published, filepath.Join(secondRoot, "session-2"))
	if err != nil {
		t.Fatal(err)
	}
	if second.StartRevision != published {
		t.Fatalf("second Runner start=%q want=%q", second.StartRevision, published)
	}
	if body, err := os.ReadFile(filepath.Join(second.WorktreePath, "runner.txt")); err != nil || string(body) != "first runner\n" {
		t.Fatalf("continued branch content=%q err=%v", body, err)
	}
}

func TestRemoteRepositoryManagerRejectsUnexpectedRemoteAdvance(t *testing.T) {
	origin, _, _ := initRemoteRepository(t)
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	checkout, err := manager.Prepare(context.Background(), origin, "", "agent-board/AB-3", "", filepath.Join(root, "session-1"))
	if err != nil {
		t.Fatal(err)
	}
	published, err := manager.Publish(context.Background(), origin, checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Cleanup(context.Background(), origin, checkout); err != nil {
		t.Fatal(err)
	}

	other := filepath.Join(t.TempDir(), "other")
	runRemoteGit(t, "", "clone", "-q", origin, other)
	runRemoteGit(t, other, "checkout", "-qb", "agent-board/AB-3", "origin/agent-board/AB-3")
	if err := os.WriteFile(filepath.Join(other, "unexpected.txt"), []byte("advance\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRemoteGit(t, other, "add", "unexpected.txt")
	runRemoteGit(t, other, "-c", "user.name=Other", "-c", "user.email=other@example.invalid", "commit", "-qm", "unexpected advance")
	runRemoteGit(t, other, "push", "-q", "origin", "agent-board/AB-3")

	if _, err := manager.Prepare(context.Background(), origin, "", "agent-board/AB-3", published, filepath.Join(root, "session-2")); err == nil {
		t.Fatal("unexpected remote Issue branch advance was accepted")
	}
}

func TestRemoteRepositoryManagerPushFailureRetainsWorktree(t *testing.T) {
	origin, _, _ := initRemoteRepository(t)
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	checkout, err := manager.Prepare(context.Background(), origin, "", "agent-board/AB-4", "", filepath.Join(root, "session-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout.WorktreePath, "partial.txt"), []byte("partial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingOrigin := origin + ".missing"
	if err := os.Rename(origin, missingOrigin); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(missingOrigin, origin)

	if _, err := manager.Publish(context.Background(), origin, checkout); err == nil {
		t.Fatal("publish unexpectedly succeeded with missing origin")
	}
	if _, err := os.Stat(checkout.WorktreePath); err != nil {
		t.Fatalf("push failure removed recovery worktree: %v", err)
	}
	if got := remoteGitOutput(t, checkout.WorktreePath, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("partial work was not committed before failed push: %q", got)
	}
}

func TestRemoteRepositoryManagerSerializesSameRepositoryMutations(t *testing.T) {
	origin, _, _ := initRemoteRepository(t)
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for index, branch := range []string{"agent-board/AB-20", "agent-board/AB-21"} {
		wg.Add(1)
		go func(index int, branch string) {
			defer wg.Done()
			_, err := manager.Prepare(context.Background(), origin, "main", branch, "", filepath.Join(root, "session-"+string(rune('a'+index))))
			errs <- err
		}(index, branch)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent same-repository prepare: %v", err)
		}
	}
}

func initRemoteRepository(t *testing.T) (origin, working, mainRevision string) {
	t.Helper()
	working = filepath.Join(t.TempDir(), "working")
	runRemoteGit(t, "", "init", "-q", "-b", "main", working)
	if err := os.WriteFile(filepath.Join(working, "README.md"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRemoteGit(t, working, "add", "README.md")
	runRemoteGit(t, working, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "main")
	mainRevision = remoteGitOutput(t, working, "rev-parse", "HEAD")
	origin = filepath.Join(t.TempDir(), "origin.git")
	runRemoteGit(t, "", "clone", "--bare", "-q", working, origin)
	runRemoteGit(t, working, "remote", "add", "origin", origin)
	return origin, working, mainRevision
}

func remoteGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := remoteGitOutputError(dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(out)
}

func remoteGitOutputError(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runRemoteGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = remoteGitOutput(t, dir, args...)
}
