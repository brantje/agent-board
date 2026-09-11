package workspacegit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFinalizeCheckoutOnBranchFinalizesExpectedIssueBranch(t *testing.T) {
	repo := initRepository(t)
	const branch = "agent-board/AB-40"
	runGitTest(t, repo, "checkout", "-qb", branch)
	start := gitOutput(t, repo, "rev-parse", "HEAD")
	writeTestFile(t, filepath.Join(repo, "leftover.txt"), "leftover\n", 0o644)

	head, err := FinalizeCheckoutOnBranch(t.Context(), repo, branch, start, "git", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if head == start {
		t.Fatal("leftover work was not committed")
	}
	if got := gitOutput(t, repo, "symbolic-ref", "--short", "HEAD"); got != branch {
		t.Fatalf("branch=%q want=%q", got, branch)
	}
	if got := gitOutput(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("workspace not clean after finalization: %q", got)
	}
}

func TestFinalizeCheckoutOnBranchRejectsDescendantOnDifferentBranch(t *testing.T) {
	repo := initRepository(t)
	const branch = "agent-board/AB-41"
	runGitTest(t, repo, "checkout", "-qb", branch)
	start := gitOutput(t, repo, "rev-parse", "HEAD")

	runGitTest(t, repo, "checkout", "-qb", "other-branch")
	writeTestFile(t, filepath.Join(repo, "other.txt"), "other branch\n", 0o644)
	runGitTest(t, repo, "add", "other.txt")
	runGitTest(t, repo, "-c", "user.name=Agent", "-c", "user.email=agent@example.invalid", "commit", "-qm", "other branch descendant")
	otherHead := gitOutput(t, repo, "rev-parse", "HEAD")
	if got := gitOutput(t, repo, "merge-base", "--is-ancestor", start, otherHead); got != "" {
		t.Fatalf("test setup did not create descendant head: %q", got)
	}

	_, err := FinalizeCheckoutOnBranch(t.Context(), repo, branch, start, "git", 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), "workspace branch changed") {
		t.Fatalf("wrong-branch finalization error=%v", err)
	}
	if got := gitOutput(t, repo, "rev-parse", "HEAD"); got != otherHead {
		t.Fatalf("rejected finalization moved HEAD: got=%q want=%q", got, otherHead)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "other.txt")); statErr != nil {
		t.Fatalf("rejected finalization damaged checkout: %v", statErr)
	}
}
