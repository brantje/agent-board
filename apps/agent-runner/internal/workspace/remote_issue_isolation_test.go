package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteRepositoryManagerIsolatesTwoIssueBranchesInSameProject(t *testing.T) {
	origin, _, mainRevision := initRemoteRepository(t)
	root := t.TempDir()
	manager := NewRemoteRepositoryManager(root)
	ctx := context.Background()

	first, err := manager.Prepare(ctx, origin, "main", "agent-board/AB-70", "", filepath.Join(root, "issue-70"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Prepare(ctx, origin, "main", "agent-board/AB-71", "", filepath.Join(root, "issue-71"))
	if err != nil {
		t.Fatal(err)
	}
	if first.CachePath != second.CachePath {
		t.Fatalf("same Project repository used different caches: %q != %q", first.CachePath, second.CachePath)
	}

	if err := os.WriteFile(filepath.Join(first.WorktreePath, "issue-70.txt"), []byte("issue 70\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstRevision, err := manager.Publish(ctx, origin, first)
	if err != nil {
		t.Fatal(err)
	}
	if firstRevision == mainRevision {
		t.Fatal("first Issue branch did not advance")
	}

	// Git worktree ownership must prevent one Issue checkout from moving the
	// branch currently owned by the other Issue checkout.
	if _, err := remoteGitOutputError(first.WorktreePath, "branch", "-f", second.Branch, firstRevision); err == nil {
		t.Fatal("first Issue checkout was able to reset the second Issue branch")
	}
	if got := remoteGitOutput(t, second.WorktreePath, "rev-parse", "HEAD"); got != mainRevision {
		t.Fatalf("second Issue checkout moved: got=%q want=%q", got, mainRevision)
	}
	if got := remoteGitOutput(t, second.CachePath, "rev-parse", "refs/heads/"+second.Branch); got != mainRevision {
		t.Fatalf("second Issue cache branch moved: got=%q want=%q", got, mainRevision)
	}

	if err := os.WriteFile(filepath.Join(second.WorktreePath, "issue-71.txt"), []byte("issue 71\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondRevision, err := manager.Publish(ctx, origin, second)
	if err != nil {
		t.Fatal(err)
	}
	if secondRevision == mainRevision || secondRevision == firstRevision {
		t.Fatalf("second Issue revision=%q main=%q first=%q", secondRevision, mainRevision, firstRevision)
	}
	if got := remoteGitOutput(t, origin, "rev-parse", "refs/heads/"+first.Branch); got != firstRevision {
		t.Fatalf("publishing second Issue mutated first remote branch: got=%q want=%q", got, firstRevision)
	}
	if got := remoteGitOutput(t, origin, "rev-parse", "refs/heads/"+second.Branch); got != secondRevision {
		t.Fatalf("second remote branch=%q want=%q", got, secondRevision)
	}
}
