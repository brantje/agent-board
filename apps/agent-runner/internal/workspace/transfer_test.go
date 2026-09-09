package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMaterializeAndSnapshotBundleRoundTrip(t *testing.T) {
	source := t.TempDir()
	runGitCLI(t, "-C", source, "init")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCLI(t, "-C", source, "add", "README.md")
	runGitCLI(t, "-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")

	bundlePath := filepath.Join(t.TempDir(), "workspace.bundle")
	runGitCLI(t, "-C", source, "bundle", "create", bundlePath, "HEAD")
	payload, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "session-1")
	if err := MaterializeBundle(context.Background(), destination, payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncPayload, err := SnapshotBundle(context.Background(), destination, "sync-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(syncPayload) == 0 {
		t.Fatal("expected non-empty sync bundle")
	}
}

func runGitCLI(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
