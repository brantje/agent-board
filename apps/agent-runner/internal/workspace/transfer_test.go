package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeBranchBundleUsesAdvertisedBranchAndHead(t *testing.T) {
	ctx := context.Background()
	source := initRunnerTransferRepository(t)
	payload, err := SnapshotBundle(ctx, source, "transfer-1")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "session")
	state, err := MaterializeBranchBundle(ctx, destination, payload)
	if err != nil {
		t.Fatal(err)
	}
	if state.Branch != "agent-board/AB-12" {
		t.Fatalf("branch=%q", state.Branch)
	}
	if got := runnerTransferOutput(t, destination, "symbolic-ref", "--short", "HEAD"); got != state.Branch {
		t.Fatalf("checked out branch=%q want=%q", got, state.Branch)
	}
	if got := runnerTransferOutput(t, destination, "rev-parse", "HEAD"); got != state.StartRevision {
		t.Fatalf("head=%q want=%q", got, state.StartRevision)
	}
}

func TestFinalizeCheckoutCommitsRunnerLeftoversBeforeSnapshot(t *testing.T) {
	ctx := context.Background()
	source := initRunnerTransferRepository(t)
	payload, err := SnapshotBundle(ctx, source, "transfer-1")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "session")
	state, err := MaterializeBranchBundle(ctx, destination, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "runner.txt"), []byte("runner change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	head, err := FinalizeCheckout(ctx, destination, state)
	if err != nil {
		t.Fatal(err)
	}
	if head == state.StartRevision {
		t.Fatal("leftover change did not create a commit")
	}
	if got := runnerTransferOutput(t, destination, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("finalized status=%q", got)
	}
	if _, err := SnapshotBundle(ctx, destination, "transfer-2"); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeCheckoutRejectsBranchSwitch(t *testing.T) {
	ctx := context.Background()
	source := initRunnerTransferRepository(t)
	payload, err := SnapshotBundle(ctx, source, "transfer-1")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "session")
	state, err := MaterializeBranchBundle(ctx, destination, payload)
	if err != nil {
		t.Fatal(err)
	}
	runRunnerTransferGit(t, destination, "checkout", "-qb", "other")
	if _, err := FinalizeCheckout(ctx, destination, state); err == nil {
		t.Fatal("branch switch was accepted")
	}
}

func TestMaterializeBranchBundleRejectsInvalidOrEmptyPayload(t *testing.T) {
	for name, payload := range map[string][]byte{"empty": nil, "invalid": []byte("not a git bundle")} {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "session")
			if _, err := MaterializeBranchBundle(context.Background(), destination, payload); err == nil {
				t.Fatal("invalid payload accepted")
			}
			if IsRepository(context.Background(), destination) {
				t.Fatal("invalid payload left a Git checkout")
			}
		})
	}
}

func initRunnerTransferRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runRunnerTransferGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRunnerTransferGit(t, repo, "add", ".")
	runRunnerTransferGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "init")
	runRunnerTransferGit(t, repo, "checkout", "-qb", "agent-board/AB-12")
	return repo
}

func runnerTransferOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func runRunnerTransferGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = runnerTransferOutput(t, dir, args...)
}
