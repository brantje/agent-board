package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func newProvisionerGit(t *testing.T) *workspace.GitCLI {
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	return git
}

func TestProvisionerCreatesAndInitializesMissingRepository(t *testing.T) {
	root := t.TempDir()
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	git := newProvisionerGit(t)
	provisioner, err := NewProvisioner(policy, git)
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, "widget")
	got, err := provisioner.EnsureProjectRepository(context.Background(), candidate, "main")
	if err != nil {
		t.Fatalf("EnsureProjectRepository() error = %v", err)
	}
	want, _ := filepath.EvalSymlinks(candidate)
	if got != want {
		t.Fatalf("EnsureProjectRepository() = %q, want %q", got, want)
	}
	isRepo, err := git.IsRepository(context.Background(), got)
	if err != nil || !isRepo {
		t.Fatalf("IsRepository() = %v %v, want true", isRepo, err)
	}
}

func TestProvisionerRejectsUnauthorizedRepositoryPath(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	git := newProvisionerGit(t)
	provisioner, err := NewProvisioner(policy, git)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provisioner.EnsureProjectRepository(context.Background(), outside, "main"); err == nil {
		t.Fatal("EnsureProjectRepository() unexpectedly succeeded outside authorized roots")
	}
}
