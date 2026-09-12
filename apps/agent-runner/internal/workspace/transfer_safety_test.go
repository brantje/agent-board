package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchTransferRejectsInvalidAndDirtyState(t *testing.T) {
	ctx := context.Background()
	if _, err := MaterializeBranchBundle(ctx, filepath.Join(t.TempDir(), "session"), nil); err == nil {
		t.Fatal("empty branch bundle was accepted")
	}
	if _, err := MaterializeBranchBundle(ctx, filepath.Join(t.TempDir(), "session"), []byte("not a git bundle")); err == nil {
		t.Fatal("invalid branch bundle was accepted")
	}

	repo := initRunnerTransferRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotBundle(ctx, repo, "dirty-transfer"); err == nil || !strings.Contains(err.Error(), "clean") {
		t.Fatalf("dirty workspace snapshot error=%v", err)
	}
}

func TestFinalizeCheckoutRejectsBranchSwitchAndMissingRepository(t *testing.T) {
	ctx := context.Background()
	source := initRunnerTransferRepository(t)
	payload, err := SnapshotBundle(ctx, source, "branch-switch")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "session")
	state, err := MaterializeBranchBundle(ctx, destination, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, "-C", destination, "checkout", "-q", "-b", "agent-board/other"); err != nil {
		t.Fatal(err)
	}
	if _, err := FinalizeCheckout(ctx, destination, state); err == nil || !strings.Contains(err.Error(), "branch changed") {
		t.Fatalf("branch switch finalization error=%v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	if IsRepository(ctx, missing) {
		t.Fatal("missing path reported as Git repository")
	}
	if _, err := FinalizeCheckout(ctx, missing, CheckoutState{Branch: "agent-board/AB-1", StartRevision: strings.Repeat("a", 40)}); err == nil {
		t.Fatal("missing repository finalized")
	}
}

func TestRunGitHandlesNilContextAndReportsGitFailure(t *testing.T) {
	repo := initRunnerTransferRepository(t)
	if got, err := runGit(nil, "-C", repo, "rev-parse", "--is-inside-work-tree"); err != nil || got != "true" {
		t.Fatalf("nil-context git result=%q err=%v", got, err)
	}
	if _, err := runGit(context.Background(), "-C", repo, "rev-parse", "--verify", "refs/heads/does-not-exist"); err == nil {
		t.Fatal("invalid Git ref unexpectedly resolved")
	}
}
