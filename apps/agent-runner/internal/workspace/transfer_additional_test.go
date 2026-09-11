package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeBundleCompatibilityWrapper(t *testing.T) {
	ctx := context.Background()
	source := initRunnerTransferRepository(t)
	payload, err := SnapshotBundle(ctx, source, "transfer-wrapper")
	if err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "session")
	if err := MaterializeBundle(ctx, destination, payload); err != nil {
		t.Fatal(err)
	}
	if got := runnerTransferOutput(t, destination, "symbolic-ref", "--short", "HEAD"); got != "agent-board/AB-12" {
		t.Fatalf("checked out branch=%q", got)
	}
}

func TestFinalizeCheckoutLeavesCleanBranchAtStartRevision(t *testing.T) {
	ctx := context.Background()
	source := initRunnerTransferRepository(t)
	payload, err := SnapshotBundle(ctx, source, "transfer-clean")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "session")
	state, err := MaterializeBranchBundle(ctx, destination, payload)
	if err != nil {
		t.Fatal(err)
	}

	head, err := FinalizeCheckout(ctx, destination, state)
	if err != nil {
		t.Fatal(err)
	}
	if head != state.StartRevision {
		t.Fatalf("clean checkout head=%q want=%q", head, state.StartRevision)
	}
}

func TestMaterializeBranchBundleRejectsUncreatableParent(t *testing.T) {
	ctx := context.Background()
	source := initRunnerTransferRepository(t)
	payload, err := SnapshotBundle(ctx, source, "transfer-parent")
	if err != nil {
		t.Fatal(err)
	}

	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeBranchBundle(ctx, filepath.Join(parent, "session"), payload); err == nil {
		t.Fatal("materialization unexpectedly succeeded beneath a file")
	}
}

func TestMaterializeBranchBundleReplacesExistingDestination(t *testing.T) {
	ctx := context.Background()
	source := initRunnerTransferRepository(t)
	payload, err := SnapshotBundle(ctx, source, "transfer-replace")
	if err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(destination, "stale.txt")
	if err := os.WriteFile(stale, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeBranchBundle(ctx, destination, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale destination content survived materialization: %v", err)
	}
}
