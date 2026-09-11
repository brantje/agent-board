package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyTransferBundlePreservesAuthoritativeGitState(t *testing.T) {
	ctx := context.Background()
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	source := initTransferRepository(t)
	destination := cloneRepository(t, source)

	// This staging existed before Runner execution and remains server-owned.
	writeTransferFile(t, filepath.Join(source, "preexisting.txt"), "staged before runner\n")
	runTransferGit(t, source, "add", "preexisting.txt")
	writeTransferFile(t, filepath.Join(destination, "preexisting.txt"), "staged before runner\n")
	runTransferGit(t, destination, "add", "preexisting.txt")
	headBefore := gitOutput(t, destination, "rev-parse", "HEAD")
	stagedBefore := gitOutput(t, destination, "diff", "--cached", "--binary")

	// Runner-owned filesystem changes may include staging, but Runner staging is
	// transport input and must not replace the authoritative server index.
	writeTransferFile(t, filepath.Join(source, "tracked.txt"), "runner changed\n")
	runTransferGit(t, source, "add", "tracked.txt")
	if err := os.Remove(filepath.Join(source, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	writeTransferFile(t, filepath.Join(source, "new.txt"), "runner new\n")

	payload, err := git.TransferSnapshot(ctx, source, "sync")
	if err != nil {
		t.Fatal(err)
	}
	if err := git.ApplyTransferBundle(ctx, destination, payload); err != nil {
		t.Fatal(err)
	}

	if got := gitOutput(t, destination, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("sync-back advanced authoritative HEAD: before=%s after=%s", headBefore, got)
	}
	if got := gitOutput(t, destination, "diff", "--cached", "--binary"); got != stagedBefore {
		t.Fatalf("sync-back changed authoritative staging\nwant=%s\ngot=%s", stagedBefore, got)
	}
	if body, err := os.ReadFile(filepath.Join(destination, "tracked.txt")); err != nil || string(body) != "runner changed\n" {
		t.Fatalf("tracked Runner change missing: %q %v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(destination, "new.txt")); err != nil || string(body) != "runner new\n" {
		t.Fatalf("untracked Runner change missing: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "deleted.txt")); !os.IsNotExist(err) {
		t.Fatalf("Runner deletion missing: %v", err)
	}
}

func TestApplyTransferBundleRejectsDifferentAuthoritativeHEAD(t *testing.T) {
	ctx := context.Background()
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	source := initTransferRepository(t)
	payload, err := git.TransferSnapshot(ctx, source, "stale-baseline")
	if err != nil {
		t.Fatal(err)
	}

	destination := cloneRepository(t, source)
	writeTransferFile(t, filepath.Join(destination, "server-only.txt"), "concurrent\n")
	runTransferGit(t, destination, "add", "server-only.txt")
	runTransferGit(t, destination, "commit", "-qm", "concurrent server commit")

	if err := git.ApplyTransferBundle(ctx, destination, payload); err == nil {
		t.Fatal("sync-back accepted a transfer from a different authoritative HEAD")
	}
}

func initTransferRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runTransferGit(t, repository, "init", "-q")
	runTransferGit(t, repository, "config", "user.email", "test@example.invalid")
	runTransferGit(t, repository, "config", "user.name", "Agent Board Test")
	writeTransferFile(t, filepath.Join(repository, "tracked.txt"), "base\n")
	writeTransferFile(t, filepath.Join(repository, "deleted.txt"), "delete me\n")
	writeTransferFile(t, filepath.Join(repository, "preexisting.txt"), "base\n")
	runTransferGit(t, repository, "add", ".")
	runTransferGit(t, repository, "commit", "-qm", "baseline")
	return repository
}

func cloneRepository(t *testing.T, source string) string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "clone")
	cmd := exec.Command("git", "clone", "-q", source, destination)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone repository: %v\n%s", err, output)
	}
	runTransferGit(t, destination, "config", "user.email", "test@example.invalid")
	runTransferGit(t, destination, "config", "user.name", "Agent Board Test")
	return destination
}

func runTransferGit(t *testing.T, dir string, args ...string) {
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

func writeTransferFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
