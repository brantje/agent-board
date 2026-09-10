package workspace

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestApplyTransferBundlePreservesModesSymlinksAndAuthoritativeIndex(t *testing.T) {
	ctx := context.Background()
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	runTransferGit(t, source, "init", "-q", "-b", "main")
	runTransferGit(t, source, "config", "user.email", "test@example.invalid")
	runTransferGit(t, source, "config", "user.name", "Agent Board Test")
	writeMode(t, filepath.Join(source, "script.sh"), "#!/bin/sh\necho ok\n", 0o644)
	writeMode(t, filepath.Join(source, "first.txt"), "first\n", 0o644)
	writeMode(t, filepath.Join(source, "second.txt"), "second\n", 0o644)
	if err := os.Symlink("first.txt", filepath.Join(source, "current.txt")); err != nil {
		t.Fatal(err)
	}
	runTransferGit(t, source, "add", ".")
	runTransferGit(t, source, "commit", "-qm", "baseline")

	destination := filepath.Join(t.TempDir(), "authoritative")
	cmd := exec.Command("git", "clone", source, destination)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone baseline: %v: %s", err, output)
	}
	indexBefore, err := os.ReadFile(filepath.Join(destination, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	headBefore := gitOutput(t, destination, "rev-parse", "HEAD")

	if err := os.Chmod(filepath.Join(source, "script.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "current.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("second.txt", filepath.Join(source, "current.txt")); err != nil {
		t.Fatal(err)
	}
	payload, err := git.TransferSnapshot(ctx, source, "mode-symlink-sync")
	if err != nil {
		t.Fatal(err)
	}
	if err := git.ApplyTransferBundle(ctx, destination, payload); err != nil {
		t.Fatal(err)
	}

	if got := gitOutput(t, destination, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD changed: before=%s after=%s", headBefore, got)
	}
	indexAfter, err := os.ReadFile(filepath.Join(destination, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("mode/symlink sync mutated authoritative .git/index")
	}
	info, err := os.Stat(filepath.Join(destination, "script.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("Runner executable bit was not applied: mode=%v", info.Mode())
	}
	target, err := os.Readlink(filepath.Join(destination, "current.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "second.txt" {
		t.Fatalf("Runner symlink target=%q want second.txt", target)
	}
}
