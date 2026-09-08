package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func TestNewProvisionerRequiresDependencies(t *testing.T) {
	policy, err := NewPolicy([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvisioner(nil, git); err == nil {
		t.Fatal("NewProvisioner() unexpectedly accepted nil policy")
	}
	if _, err := NewProvisioner(policy, nil); err == nil {
		t.Fatal("NewProvisioner() unexpectedly accepted nil git")
	}
}

func TestProvisionerUsesMainWhenDefaultBranchBlank(t *testing.T) {
	root := t.TempDir()
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := NewProvisioner(policy, git)
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, "blank-branch")
	got, err := provisioner.EnsureProjectRepository(context.Background(), candidate, "   ")
	if err != nil {
		t.Fatalf("EnsureProjectRepository() error = %v", err)
	}
	branch, err := git.CurrentBranch(context.Background(), got)
	if err != nil || branch != "main" {
		t.Fatalf("CurrentBranch() = %q err=%v, want main", branch, err)
	}
}

func TestProvisionerRejectsInvalidDefaultBranch(t *testing.T) {
	root := t.TempDir()
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := NewProvisioner(policy, git)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provisioner.EnsureProjectRepository(context.Background(), filepath.Join(root, "widget"), "bad branch"); err == nil {
		t.Fatal("EnsureProjectRepository() unexpectedly accepted invalid branch")
	}
}

func TestPolicyEnsureRequiresAuthorizedRoots(t *testing.T) {
	policy, _ := NewPolicy(nil)
	if _, err := policy.Ensure(t.TempDir()); err != ErrNoAuthorizedRoots {
		t.Fatalf("Ensure() error = %v, want ErrNoAuthorizedRoots", err)
	}
}

func TestPolicyEnsureLeavesExistingAuthorizedDirectory(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "existing")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(repo, "keep.txt")
	if err := os.WriteFile(marker, []byte("stay"), 0o644); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	got, err := policy.Ensure(repo)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if got != repo {
		t.Fatalf("Ensure() = %q, want %q", got, repo)
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "stay" {
		t.Fatalf("Ensure() modified existing directory contents: %q err=%v", content, err)
	}
}
