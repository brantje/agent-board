package workspacegit

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotBundlePreservesAuthoritativeIndexAndCapturesFilesystem(t *testing.T) {
	repository := t.TempDir()
	runGitTest(t, repository, "init", "-q")
	runGitTest(t, repository, "config", "user.email", "test@example.invalid")
	runGitTest(t, repository, "config", "user.name", "Agent Board Test")
	writeTestFile(t, filepath.Join(repository, "staged.txt"), "base staged\n", 0o644)
	writeTestFile(t, filepath.Join(repository, "unstaged.txt"), "base unstaged\n", 0o644)
	writeTestFile(t, filepath.Join(repository, "deleted.txt"), "delete me\n", 0o644)
	writeTestFile(t, filepath.Join(repository, "exec.sh"), "#!/bin/sh\necho ok\n", 0o755)
	writeTestFile(t, filepath.Join(repository, ".gitignore"), "ignored.txt\nbuild/\n", 0o644)
	if err := os.Symlink("staged.txt", filepath.Join(repository, "link.txt")); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repository, "add", ".")
	runGitTest(t, repository, "commit", "-qm", "baseline")

	writeTestFile(t, filepath.Join(repository, "staged.txt"), "staged change\n", 0o644)
	runGitTest(t, repository, "add", "staged.txt")
	writeTestFile(t, filepath.Join(repository, "unstaged.txt"), "unstaged change\n", 0o644)
	if err := os.Remove(filepath.Join(repository, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "new.txt"), "new change\n", 0o644)
	writeTestFile(t, filepath.Join(repository, "ignored.txt"), "secret\n", 0o644)
	if err := os.Mkdir(filepath.Join(repository, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "build", "ignored.bin"), "cache\n", 0o644)

	indexPath := filepath.Join(repository, ".git", "index")
	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	stagedBefore := gitOutput(t, repository, "diff", "--cached", "--binary")
	statusBefore := gitOutput(t, repository, "status", "--porcelain=v1")
	headBefore := gitOutput(t, repository, "rev-parse", "HEAD")

	payload, err := SnapshotBundle(context.Background(), repository, "../../unsafe transfer id", "git", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	indexAfter, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("authoritative .git/index changed")
	}
	if got := gitOutput(t, repository, "diff", "--cached", "--binary"); got != stagedBefore {
		t.Fatalf("staged state changed\nbefore=%s\nafter=%s", stagedBefore, got)
	}
	if got := gitOutput(t, repository, "status", "--porcelain=v1"); got != statusBefore {
		t.Fatalf("workspace status changed\nbefore=%s\nafter=%s", statusBefore, got)
	}
	if got := gitOutput(t, repository, "rev-parse", "HEAD"); got != headBefore {
		t.Fatal("snapshot changed HEAD")
	}
	if refs := gitOutput(t, repository, "for-each-ref", "--format=%(refname)", "refs/agent-board/transfer/"); refs != "" {
		t.Fatalf("transfer ref leaked: %s", refs)
	}
	if strings.Contains(gitOutput(t, repository, "log", "--oneline"), "Agent Board workspace transfer") {
		t.Fatal("temporary transfer commit became visible history")
	}

	clone := cloneBundle(t, payload)
	for path, want := range map[string]string{
		"staged.txt":   "staged change\n",
		"unstaged.txt": "unstaged change\n",
		"new.txt":      "new change\n",
	} {
		body, err := os.ReadFile(filepath.Join(clone, path))
		if err != nil || string(body) != want {
			t.Fatalf("%s=%q err=%v", path, body, err)
		}
	}
	if _, err := os.Stat(filepath.Join(clone, "deleted.txt")); !os.IsNotExist(err) {
		t.Fatalf("tracked deletion missing from snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clone, "ignored.txt")); !os.IsNotExist(err) {
		t.Fatalf("ignored file transferred: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clone, "build", "ignored.bin")); !os.IsNotExist(err) {
		t.Fatalf("ignored directory transferred: %v", err)
	}
	info, err := os.Stat(filepath.Join(clone, "exec.sh"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("executable mode lost: %v %v", info, err)
	}
	target, err := os.Readlink(filepath.Join(clone, "link.txt"))
	if err != nil || target != "staged.txt" {
		t.Fatalf("symlink lost: %q %v", target, err)
	}
}

func TestSnapshotBundleRejectsInvalidInputs(t *testing.T) {
	if _, err := SnapshotBundle(context.Background(), t.TempDir(), " ", "git", time.Second); err == nil {
		t.Fatal("blank transfer id accepted")
	}
	if _, err := SnapshotBundle(context.Background(), t.TempDir(), "transfer", "git", 0); err == nil {
		t.Fatal("zero timeout accepted")
	}
	if got := safeTransferComponent("../../x"); strings.Contains(got, "/") || len(got) != 64 {
		t.Fatalf("unsafe transfer component %q", got)
	}
}

func cloneBundle(t *testing.T, payload []byte) string {
	t.Helper()
	bundle := filepath.Join(t.TempDir(), "workspace.bundle")
	if err := os.WriteFile(bundle, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "clone")
	cmd := exec.Command("git", "clone", "-q", bundle, destination)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone bundle: %v\n%s", err, output)
	}
	return destination
}

func runGitTest(t *testing.T, dir string, args ...string) {
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

func writeTestFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
