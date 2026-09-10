package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeAndSnapshotBundleRoundTrip(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	runGitCLI(t, "-C", source, "init")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "unstaged.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCLI(t, "-C", source, "add", "README.md", "unstaged.txt")
	runGitCLI(t, "-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")

	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCLI(t, "-C", source, "add", "README.md")
	if err := os.WriteFile(filepath.Join(source, "unstaged.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceHead := strings.TrimSpace(gitOutput(t, source, "rev-parse", "HEAD"))
	sourceStatus := strings.TrimSpace(gitOutput(t, source, "status", "--porcelain=v1"))

	payload, err := SnapshotBundle(ctx, source, "materialize-1")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "session-1")
	if err := MaterializeBundle(ctx, destination, payload); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, destination, "rev-parse", "HEAD")); got != sourceHead {
		t.Fatalf("materialized HEAD=%s want=%s", got, sourceHead)
	}
	if got := strings.TrimSpace(gitOutput(t, destination, "status", "--porcelain=v1")); got != sourceStatus {
		t.Fatalf("materialized status mismatch\nwant=%s\ngot=%s", sourceStatus, got)
	}

	if err := os.WriteFile(filepath.Join(destination, "roundtrip.txt"), []byte("runner change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncStatus := strings.TrimSpace(gitOutput(t, destination, "status", "--porcelain=v1"))
	syncPayload, err := SnapshotBundle(ctx, destination, "sync-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(syncPayload) == 0 {
		t.Fatal("expected non-empty sync bundle")
	}
	if refs := gitOutput(t, destination, "for-each-ref", "refs/agent-board/"); strings.TrimSpace(refs) != "" {
		t.Fatalf("sync ref leaked: %s", refs)
	}
	if !IsRepository(ctx, destination) {
		t.Fatal("materialized workspace should be a git repository")
	}
	if TransferChecksum(syncPayload) == TransferChecksum(nil) {
		t.Fatal("checksum ignored payload")
	}

	roundTrip := filepath.Join(t.TempDir(), "session-2")
	if err := MaterializeBundle(ctx, roundTrip, syncPayload); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, roundTrip, "status", "--porcelain=v1")); got != syncStatus {
		t.Fatalf("round-trip status mismatch\nwant=%s\ngot=%s", syncStatus, got)
	}
}

func TestMaterializeEmptyBundleCreatesDirectory(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "empty")
	if err := MaterializeBundle(context.Background(), destination, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil || !info.IsDir() {
		t.Fatalf("empty materialize: info=%v err=%v", info, err)
	}
}

func TestSnapshotBundleIncludesUntrackedFileAndRequiresTransferID(t *testing.T) {
	source := t.TempDir()
	runGitCLI(t, "-C", source, "init")
	if err := os.WriteFile(filepath.Join(source, "tracked.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCLI(t, "-C", source, "add", "tracked.txt")
	runGitCLI(t, "-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(source, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotBundle(context.Background(), source, ""); err == nil {
		t.Fatal("blank transfer id accepted")
	}
	payload, err := SnapshotBundle(context.Background(), source, "sync-untracked")
	if err != nil || len(payload) == 0 {
		t.Fatalf("snapshot: len=%d err=%v", len(payload), err)
	}
	destination := filepath.Join(t.TempDir(), "applied")
	if err := MaterializeBundle(context.Background(), destination, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "untracked.txt")); err != nil {
		t.Fatal("untracked file missing from sync bundle")
	}
	if IsRepository(context.Background(), t.TempDir()) {
		t.Fatal("empty directory reported as repository")
	}
}

func TestMaterializeBundleRejectsInvalidPayload(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "invalid")
	if err := MaterializeBundle(context.Background(), destination, []byte("not a git bundle")); err == nil {
		t.Fatal("invalid bundle accepted")
	}
	if _, err := SnapshotBundle(context.Background(), t.TempDir(), "sync-missing"); err == nil {
		t.Fatal("snapshot of non-repository accepted")
	}
	emptyRepo := t.TempDir()
	runGitCLI(t, "-C", emptyRepo, "init")
	if _, err := SnapshotBundle(context.Background(), emptyRepo, "sync-empty"); err == nil {
		t.Fatal("snapshot of empty repository accepted")
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func runGitCLI(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
