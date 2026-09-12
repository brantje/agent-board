package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

func TestApplyTransferBundleFastForwardsIssueBranch(t *testing.T) {
	ctx := context.Background()
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	authoritative := initBranchTransferRepository(t, "agent-board/AB-12")
	runner := cloneBranchTransferRepository(t, authoritative)
	start := branchTransferOutput(t, runner, "rev-parse", "HEAD")
	writeBranchTransferFile(t, filepath.Join(runner, "agent.txt"), "agent commit\n", 0o644)
	runBranchTransferGit(t, runner, "add", "agent.txt")
	runBranchTransferGit(t, runner, "-c", "user.name=Agent", "-c", "user.email=agent@example.invalid", "commit", "-qm", "agent commit")
	writeBranchTransferFile(t, filepath.Join(runner, "leftover.txt"), "leftover\n", 0o755)
	if _, err := sharedworkspace.FinalizeCheckout(ctx, runner, start, "git", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	returnedHead := branchTransferOutput(t, runner, "rev-parse", "HEAD")
	payload, err := sharedworkspace.BranchBundle(ctx, runner, "return", "git", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.ApplyTransferBundle(ctx, authoritative, payload); err != nil {
		t.Fatal(err)
	}
	if got := branchTransferOutput(t, authoritative, "rev-parse", "HEAD"); got != returnedHead {
		t.Fatalf("authoritative head=%s want=%s", got, returnedHead)
	}
	if body, err := os.ReadFile(filepath.Join(authoritative, "agent.txt")); err != nil || string(body) != "agent commit\n" {
		t.Fatalf("agent commit missing: %q %v", body, err)
	}
	if mode := branchTransferOutput(t, authoritative, "ls-tree", "HEAD", "leftover.txt"); !strings.HasPrefix(mode, "100755 ") {
		t.Fatalf("fallback commit lost executable mode: %s", mode)
	}
}

func TestApplyTransferBundleRejectsHistoryRewritePastAuthoritativeHead(t *testing.T) {
	ctx := context.Background()
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	authoritative := initBranchTransferRepository(t, "agent-board/AB-12")
	runner := cloneBranchTransferRepository(t, authoritative)
	parent := branchTransferOutput(t, runner, "rev-parse", "HEAD^")
	runBranchTransferGit(t, runner, "reset", "--hard", parent)
	writeBranchTransferFile(t, filepath.Join(runner, "rewrite.txt"), "rewrite\n", 0o644)
	runBranchTransferGit(t, runner, "add", ".")
	runBranchTransferGit(t, runner, "-c", "user.name=Agent", "-c", "user.email=agent@example.invalid", "commit", "-qm", "rewrite")
	payload, err := sharedworkspace.BranchBundle(ctx, runner, "return", "git", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	before := branchTransferOutput(t, authoritative, "rev-parse", "HEAD")
	if err := git.ApplyTransferBundle(ctx, authoritative, payload); err == nil {
		t.Fatal("rewritten runner history was accepted")
	}
	if got := branchTransferOutput(t, authoritative, "rev-parse", "HEAD"); got != before {
		t.Fatalf("rejected import moved authoritative branch: before=%s after=%s", before, got)
	}
}

func TestApplyTransferBundleRejectsWrongIssueBranch(t *testing.T) {
	ctx := context.Background()
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	authoritative := initBranchTransferRepository(t, "agent-board/AB-12")
	runner := cloneBranchTransferRepository(t, authoritative)
	runBranchTransferGit(t, runner, "checkout", "-qb", "agent-board/AB-99")
	payload, err := sharedworkspace.BranchBundle(ctx, runner, "return", "git", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.ApplyTransferBundle(ctx, authoritative, payload); err == nil {
		t.Fatal("wrong Issue branch was accepted")
	}
}

func TestTransferSnapshotRequiresCleanIssueBoundary(t *testing.T) {
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	repo := initBranchTransferRepository(t, "agent-board/AB-12")
	writeBranchTransferFile(t, filepath.Join(repo, "dirty.txt"), "dirty\n", 0o644)
	if _, err := git.TransferSnapshot(context.Background(), repo, "outbound"); err == nil {
		t.Fatal("dirty Issue Workspace was transferred")
	}
}

func initBranchTransferRepository(t *testing.T, branch string) string {
	t.Helper()
	repository := t.TempDir()
	runBranchTransferGit(t, repository, "init", "-q")
	writeBranchTransferFile(t, filepath.Join(repository, "tracked.txt"), "base\n", 0o644)
	writeBranchTransferFile(t, filepath.Join(repository, "base.txt"), "base\n", 0o644)
	runBranchTransferGit(t, repository, "add", ".")
	runBranchTransferGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	writeBranchTransferFile(t, filepath.Join(repository, "second.txt"), "second\n", 0o644)
	runBranchTransferGit(t, repository, "add", ".")
	runBranchTransferGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "second")
	runBranchTransferGit(t, repository, "checkout", "-qb", branch)
	return repository
}

func cloneBranchTransferRepository(t *testing.T, source string) string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "clone")
	cmd := exec.Command("git", "clone", "-q", source, destination)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone repository: %v\n%s", err, output)
	}
	return destination
}

func runBranchTransferGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func branchTransferOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeBranchTransferFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
