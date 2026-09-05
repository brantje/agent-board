package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
)

func TestGitCLIValidationAndLookupFailures(t *testing.T) {
	if _, err := NewGitCLI("agent-board-git-does-not-exist"); err == nil {
		t.Fatal("NewGitCLI() unexpectedly found a nonexistent binary")
	}
	git := requireGit(t)
	if err := git.ValidateBranch(context.Background(), "   "); err == nil {
		t.Fatal("blank branch unexpectedly accepted")
	}
	if err := git.ValidateBranch(context.Background(), "bad..branch"); err == nil {
		t.Fatal("invalid Git branch unexpectedly accepted")
	}
}

func TestCommandNameHandlesWorkingDirectoryOptionsAndEmptyInput(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"-C", "/repo", "rev-parse", "HEAD"}, want: "rev-parse"},
		{args: []string{"--quiet", "status"}, want: "status"},
		{args: []string{"-C", "/repo"}, want: "command"},
		{args: nil, want: "command"},
	}
	for _, tc := range tests {
		if got := commandName(tc.args); got != tc.want {
			t.Fatalf("commandName(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestNewMaterializerRejectsMissingDependenciesAndRelativeRoot(t *testing.T) {
	if _, err := NewMaterializer(nil, nil, nil, "/tmp/workspaces"); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("missing dependencies error = %v, want ErrInvalidMetadata", err)
	}
	git := requireGit(t)
	policy, err := repository.NewPolicy([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	state := &memoryStateStore{}
	if _, err := NewMaterializer(state, policy, git, "relative/workspaces"); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("relative root error = %v, want ErrInvalidRoot", err)
	}
}

func TestBootstrapTempCleanupAndInvalidFinalReset(t *testing.T) {
	root := t.TempDir()
	workspaceID := "workspace-1"
	stale := filepath.Join(root, "."+workspaceID+".bootstrap-stale")
	other := filepath.Join(root, ".other.bootstrap-stale")
	for _, path := range []string{stale, other} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanupBootstrapTemps(root, workspaceID); err != nil {
		t.Fatalf("cleanupBootstrapTemps() error = %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale bootstrap still exists: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated bootstrap was removed: %v", err)
	}

	final := filepath.Join(root, workspaceID)
	if err := os.Mkdir(final, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeUnreadyFinal(final); err != nil {
		t.Fatalf("removeUnreadyFinal() error = %v", err)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("invalid final checkout still exists: %v", err)
	}
	if err := removeUnreadyFinal(final); err != nil {
		t.Fatalf("removeUnreadyFinal(missing) error = %v", err)
	}
}

func TestCanonicalWorkspaceRootCreatesMissingDirectory(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "nested", "workspaces")
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	policy, err := repository.NewPolicy([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	materializer, err := NewMaterializer(&memoryStateStore{}, policy, git, root)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := materializer.canonicalWorkspaceRoot()
	if err != nil {
		t.Fatalf("canonicalWorkspaceRoot() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if canonical != want {
		t.Fatalf("canonicalWorkspaceRoot() = %q, want %q", canonical, want)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("workspace root was not created: info=%v err=%v", info, err)
	}
}
