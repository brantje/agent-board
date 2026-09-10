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
	runGitCLI(t, "-C", source, "add", "README.md")
	runGitCLI(t, "-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantStatus := strings.TrimSpace(gitOutput(t, source, "status", "--porcelain=v1"))

	payload, err := SnapshotBundle(ctx, source, "transfer-1")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "session")
	if err := MaterializeBundle(ctx, destination, payload); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, destination, "status", "--porcelain=v1")); got != wantStatus {
		t.Fatalf("materialized status mismatch\nwant=%s\ngot=%s", wantStatus, got)
	}

	if err := os.WriteFile(filepath.Join(destination, "runner.txt"), []byte("runner change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncPayload, err := SnapshotBundle(ctx, destination, "transfer-2")
	if err != nil || len(syncPayload) == 0 {
		t.Fatalf("snapshot len=%d err=%v", len(syncPayload), err)
	}
	if !IsRepository(ctx, destination) {
		t.Fatal("materialized Workspace is not a Git repository")
	}

	roundTrip := filepath.Join(t.TempDir(), "round-trip")
	if err := MaterializeBundle(ctx, roundTrip, syncPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(roundTrip, "runner.txt")); err != nil {
		t.Fatalf("runner change missing from round trip: %v", err)
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
