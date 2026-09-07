package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitCLIApplyCandidatePatchHandlesStagedAndUnstagedPatches(t *testing.T) {
	git := requireGit(t).GitCLI
	repo := createFixtureRepository(t, git, t.TempDir())
	ctx := context.Background()
	readme := filepath.Join(repo, "README.md")

	if err := os.WriteFile(readme, []byte("unstaged change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unstaged := rawGitOutput(t, ctx, git, repo, "diff", "--binary")
	if err := git.resetAcceptedCheckout(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if err := git.applyCandidatePatch(ctx, repo, unstaged, false); err != nil {
		t.Fatalf("apply unstaged patch: %v", err)
	}
	content, err := os.ReadFile(readme)
	if err != nil || string(content) != "unstaged change\n" {
		t.Fatalf("content=%q err=%v", content, err)
	}

	if err := git.resetAcceptedCheckout(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readme, []byte("staged change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(ctx, "-C", repo, "add", "README.md"); err != nil {
		t.Fatal(err)
	}
	staged := rawGitOutput(t, ctx, git, repo, "diff", "--binary", "--cached", "HEAD")
	if err := git.resetAcceptedCheckout(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if err := git.applyCandidatePatch(ctx, repo, staged, true); err != nil {
		t.Fatalf("apply staged patch: %v", err)
	}
	cached, err := git.run(ctx, "-C", repo, "diff", "--cached", "--name-only")
	if err != nil || !strings.Contains(cached, "README.md") {
		t.Fatalf("cached diff=%q err=%v", cached, err)
	}
}

func TestGitCLIApplyCandidatePatchHandlesEmptyAndInvalidInput(t *testing.T) {
	git := requireGit(t).GitCLI
	repo := createFixtureRepository(t, git, t.TempDir())
	if err := git.applyCandidatePatch(t.Context(), repo, nil, false); err != nil {
		t.Fatalf("empty patch: %v", err)
	}
	if err := git.applyCandidatePatch(t.Context(), repo, []byte("not a patch\n"), false); err == nil {
		t.Fatal("invalid patch should fail")
	}
}

func rawGitOutput(t *testing.T, ctx context.Context, git *GitCLI, repo string, args ...string) []byte {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	command := exec.CommandContext(ctx, git.binary, commandArgs...)
	command.Env = hardenedGitEnv()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return output
}
