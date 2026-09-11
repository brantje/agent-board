package workspacegit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBranchBundleExportsCleanBranchAtExactHead(t *testing.T) {
	repo := initRepository(t)
	runGitTest(t, repo, "checkout", "-qb", "agent-board/AB-12")
	head := gitOutput(t, repo, "rev-parse", "HEAD")
	payload, err := BranchBundle(context.Background(), repo, "transfer", "git", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "workspace.bundle")
	if err := os.WriteFile(bundle, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	branch, revision, err := BundleHead(context.Background(), bundle, "git", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "agent-board/AB-12" || revision != head {
		t.Fatalf("bundle head=(%q,%q), want (%q,%q)", branch, revision, "agent-board/AB-12", head)
	}
	if refs := gitOutput(t, repo, "for-each-ref", "--format=%(refname)", "refs/agent-board/transfer/"); refs != "" {
		t.Fatalf("transfer ref leaked: %s", refs)
	}
}

func TestBranchBundleRejectsDirtyWorkspace(t *testing.T) {
	repo := initRepository(t)
	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "dirty\n", 0o644)
	if _, err := BranchBundle(context.Background(), repo, "transfer", "git", 30*time.Second); err == nil {
		t.Fatal("dirty workspace accepted")
	}
}

func TestFinalizeCheckoutPreservesCommitsAndCommitsLeftovers(t *testing.T) {
	repo := initRepository(t)
	start := gitOutput(t, repo, "rev-parse", "HEAD")

	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "agent commit\n", 0o644)
	runGitTest(t, repo, "add", "tracked.txt")
	runGitTest(t, repo, "-c", "user.name=Agent", "-c", "user.email=agent@example.invalid", "commit", "-qm", "agent commit")
	agentCommit := gitOutput(t, repo, "rev-parse", "HEAD")

	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "leftover\n", 0o644)
	if err := os.Remove(filepath.Join(repo, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repo, "new.txt"), "new\n", 0o644)
	writeTestFile(t, filepath.Join(repo, "ignored.txt"), "ignored\n", 0o644)
	writeTestFile(t, filepath.Join(repo, "exec.sh"), "#!/bin/sh\necho ok\n", 0o755)
	if err := os.Symlink("tracked.txt", filepath.Join(repo, "link.txt")); err != nil {
		t.Fatal(err)
	}

	head, err := FinalizeCheckout(context.Background(), repo, start, "git", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if head == agentCommit {
		t.Fatal("leftovers were not committed")
	}
	if got := gitOutput(t, repo, "merge-base", "--is-ancestor", agentCommit, head); got != "" {
		t.Fatalf("agent commit not preserved: %q", got)
	}
	if got := gitOutput(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("status=%q, want clean non-ignored workspace", got)
	}
	if body, err := os.ReadFile(filepath.Join(repo, "ignored.txt")); err != nil || string(body) != "ignored\n" {
		t.Fatalf("ignored file was not retained: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "deleted.txt")); !os.IsNotExist(err) {
		t.Fatalf("deletion not preserved: %v", err)
	}
	if mode := gitOutput(t, repo, "ls-tree", "HEAD", "exec.sh"); !strings.HasPrefix(mode, "100755 ") {
		t.Fatalf("executable mode not preserved: %s", mode)
	}
	if mode := gitOutput(t, repo, "ls-tree", "HEAD", "link.txt"); !strings.HasPrefix(mode, "120000 ") {
		t.Fatalf("symlink mode not preserved: %s", mode)
	}
	if listed := gitOutput(t, repo, "ls-tree", "-r", "--name-only", "HEAD"); strings.Contains(listed, "ignored.txt") {
		t.Fatal("ignored file committed")
	}
}

func TestFinalizeCheckoutRejectsHistoryRewritePastStart(t *testing.T) {
	repo := initRepository(t)
	start := gitOutput(t, repo, "rev-parse", "HEAD")
	parent := gitOutput(t, repo, "rev-parse", "HEAD^")
	runGitTest(t, repo, "reset", "--hard", parent)
	writeTestFile(t, filepath.Join(repo, "replacement.txt"), "rewrite\n", 0o644)
	if _, err := FinalizeCheckout(context.Background(), repo, start, "git", 30*time.Second); err == nil {
		t.Fatal("history rewrite accepted")
	}
}

func TestFinalizeCheckoutRejectsUnresolvedConflicts(t *testing.T) {
	repo := initRepository(t)
	start := gitOutput(t, repo, "rev-parse", "HEAD")
	runGitTest(t, repo, "checkout", "-qb", "other")
	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "other\n", 0o644)
	runGitTest(t, repo, "add", "tracked.txt")
	runGitTest(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "other")
	runGitTest(t, repo, "checkout", "-")
	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "ours\n", 0o644)
	runGitTest(t, repo, "add", "tracked.txt")
	runGitTest(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "ours")
	cmd := exec.Command("git", "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "merge", "other")
	cmd.Dir = repo
	if err := cmd.Run(); err == nil {
		t.Fatal("expected conflict")
	}
	if _, err := FinalizeCheckout(context.Background(), repo, start, "git", 30*time.Second); err == nil {
		t.Fatal("conflicted checkout accepted")
	}
}

func TestBranchBundleRejectsInvalidInputs(t *testing.T) {
	if _, err := BranchBundle(context.Background(), t.TempDir(), " ", "git", time.Second); err == nil {
		t.Fatal("blank transfer id accepted")
	}
	if _, err := BranchBundle(context.Background(), t.TempDir(), "transfer", "git", 0); err == nil {
		t.Fatal("zero timeout accepted")
	}
	if got := safeTransferComponent("../../x"); strings.Contains(got, "/") || len(got) != 64 {
		t.Fatalf("unsafe transfer component %q", got)
	}
}

func initRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitTest(t, repo, "init", "-q")
	writeTestFile(t, filepath.Join(repo, ".gitignore"), "ignored.txt\n", 0o644)
	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "base\n", 0o644)
	writeTestFile(t, filepath.Join(repo, "deleted.txt"), "delete me\n", 0o644)
	runGitTest(t, repo, "add", ".")
	runGitTest(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial")
	writeTestFile(t, filepath.Join(repo, "second.txt"), "second\n", 0o644)
	runGitTest(t, repo, "add", "second.txt")
	runGitTest(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "second")
	return repo
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeTestFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
