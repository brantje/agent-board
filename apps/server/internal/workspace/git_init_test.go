package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGitCLIInitRepositoryCreatesBranchAndCommit(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "new-repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := git.InitRepository(ctx, repo, "main"); err != nil {
		t.Fatalf("InitRepository() error = %v", err)
	}
	isRepo, err := git.IsRepository(ctx, repo)
	if err != nil || !isRepo {
		t.Fatalf("IsRepository() = %v %v, want true", isRepo, err)
	}
	branch, err := git.CurrentBranch(ctx, repo)
	if err != nil || branch != "main" {
		t.Fatalf("CurrentBranch() = %q err=%v, want main", branch, err)
	}
	if _, err := git.HeadRevision(ctx, repo); err != nil {
		t.Fatalf("HeadRevision() error = %v", err)
	}
}

func TestGitCLIInitRepositoryLeavesExistingRepositoryUnchanged(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "existing")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := git.runInitSequence(ctx, repo, "main"); err != nil {
		t.Fatal(err)
	}
	before, err := git.HeadRevision(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.InitRepository(ctx, repo, "main"); err != nil {
		t.Fatalf("InitRepository() error = %v", err)
	}
	after, err := git.HeadRevision(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("InitRepository() changed HEAD from %s to %s", before, after)
	}
}

func TestGitCLIInitRepositoryRejectsNonemptyNonRepositoryDirectory(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "occupied")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	if err := git.InitRepository(context.Background(), repo, "main"); !errors.Is(err, ErrNotGitRepository) {
		t.Fatalf("InitRepository() error = %v, want ErrNotGitRepository", err)
	}
}

func (g *GitCLI) runInitSequence(ctx context.Context, repositoryPath, branch string) error {
	if _, err := g.run(ctx, "-C", repositoryPath, "init", "-b", branch); err != nil {
		return err
	}
	_, err := g.run(ctx,
		"-C", repositoryPath,
		"-c", "user.name=Agent Board",
		"-c", "user.email=agent-board@localhost",
		"commit", "--allow-empty", "-m", "Initial commit",
	)
	return err
}
