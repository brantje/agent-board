package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

func TestApplyTransferBundleRejectsEmptyBundle(t *testing.T) {
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	repository := initBranchTransferRepository(t, "agent-board/AB-12")
	before := branchTransferOutput(t, repository, "rev-parse", "HEAD")
	if err := git.ApplyTransferBundle(context.Background(), repository, nil); err == nil || !strings.Contains(err.Error(), "branch bundle is required") {
		t.Fatalf("ApplyTransferBundle() error=%v", err)
	}
	if after := branchTransferOutput(t, repository, "rev-parse", "HEAD"); after != before {
		t.Fatalf("empty import moved authoritative branch: before=%s after=%s", before, after)
	}
}

func TestApplyTransferBundleRejectsDirtyAuthoritativeWorkspace(t *testing.T) {
	ctx := context.Background()
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	authoritative := initBranchTransferRepository(t, "agent-board/AB-12")
	runner := cloneBranchTransferRepository(t, authoritative)
	writeBranchTransferFile(t, filepath.Join(runner, "agent.txt"), "agent\n", 0o644)
	runBranchTransferGit(t, runner, "add", "agent.txt")
	runBranchTransferGit(t, runner, "-c", "user.name=Agent", "-c", "user.email=agent@example.invalid", "commit", "-qm", "agent")
	payload, err := sharedworkspace.BranchBundle(ctx, runner, "return", "git", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	before := branchTransferOutput(t, authoritative, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(authoritative, "tracked.txt"), []byte("dirty authoritative\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := git.ApplyTransferBundle(ctx, authoritative, payload); err == nil || !strings.Contains(err.Error(), "must be clean before branch import") {
		t.Fatalf("ApplyTransferBundle() error=%v", err)
	}
	if after := branchTransferOutput(t, authoritative, "rev-parse", "HEAD"); after != before {
		t.Fatalf("dirty import moved authoritative branch: before=%s after=%s", before, after)
	}
}

func TestApplyTransferBundleRejectsMalformedBundleWithoutMovingBranch(t *testing.T) {
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	repository := initBranchTransferRepository(t, "agent-board/AB-12")
	before := branchTransferOutput(t, repository, "rev-parse", "HEAD")
	if err := git.ApplyTransferBundle(context.Background(), repository, []byte("not a git bundle")); err == nil {
		t.Fatal("malformed runner bundle was accepted")
	}
	if after := branchTransferOutput(t, repository, "rev-parse", "HEAD"); after != before {
		t.Fatalf("malformed import moved authoritative branch: before=%s after=%s", before, after)
	}
}
