package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransferSnapshotIncludesDirtyUntrackedAndExcludesIgnored(t *testing.T) {
	ctx := context.Background()
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	runTransferGit(t, source, "init", "-q")
	runTransferGit(t, source, "config", "user.email", "test@example.invalid")
	runTransferGit(t, source, "config", "user.name", "Agent Board Test")
	writeMode(t, filepath.Join(source, "tracked.go"), "package main\n", 0o644)
	writeMode(t, filepath.Join(source, "deleted.go"), "package gone\n", 0o644)
	writeMode(t, filepath.Join(source, "exec.sh"), "#!/bin/sh\necho ok\n", 0o755)
	if err := os.Symlink("tracked.go", filepath.Join(source, "link.go")); err != nil {
		t.Fatal(err)
	}
	writeMode(t, filepath.Join(source, ".gitignore"), "ignored.txt\nbuild/\n", 0o644)
	runTransferGit(t, source, "add", ".")
	runTransferGit(t, source, "commit", "-qm", "baseline")
	headBefore := gitOutput(t, source, "rev-parse", "HEAD")
	destination := filepath.Join(t.TempDir(), "authoritative")
	cmd := exec.Command("git", "clone", source, destination)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone baseline: %v\n%s", err, out)
	}

	writeMode(t, filepath.Join(source, "tracked.go"), "package main\nfunc changed() {}\n", 0o644)
	if err := os.Remove(filepath.Join(source, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	writeMode(t, filepath.Join(source, "notes.md"), "untracked notes\n", 0o644)
	writeMode(t, filepath.Join(source, "ignored.txt"), "secret\n", 0o644)
	if err := os.Mkdir(filepath.Join(source, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMode(t, filepath.Join(source, "build", "out.bin"), "cache\n", 0o644)

	payload, err := git.TransferSnapshot(ctx, source, "xfer-1")
	if err != nil {
		t.Fatal(err)
	}
	if gitOutput(t, source, "rev-parse", "HEAD") != headBefore {
		t.Fatal("transfer snapshot mutated accepted workspace HEAD")
	}
	if strings.Contains(gitOutput(t, source, "log", "--oneline"), "Agent Board workspace transfer") {
		t.Fatal("transfer commit became visible history")
	}
	if refs := gitOutput(t, source, "for-each-ref", "refs/agent-board/transfer/"); strings.TrimSpace(refs) != "" {
		t.Fatalf("hidden transfer ref leaked: %s", refs)
	}
	if _, err := os.Stat(filepath.Join(source, "notes.md")); err != nil {
		t.Fatal("untracked file was removed from source workspace")
	}

	cloned := cloneTransferBundle(t, payload)
	if _, err := os.Stat(filepath.Join(cloned, "notes.md")); err != nil {
		t.Log(gitOutput(t, cloned, "log", "--all", "--oneline", "--decorate"))
		t.Fatal("untracked non-ignored file missing from transfer")
	}
	if _, err := os.Stat(filepath.Join(cloned, "deleted.go")); !os.IsNotExist(err) {
		t.Fatal("tracked deletion was not included")
	}
	if _, err := os.Stat(filepath.Join(cloned, "ignored.txt")); !os.IsNotExist(err) {
		t.Fatal("ignored file was transferred")
	}
	if _, err := os.Stat(filepath.Join(cloned, "build", "out.bin")); !os.IsNotExist(err) {
		t.Fatal("ignored directory was transferred")
	}
	info, err := os.Stat(filepath.Join(cloned, "exec.sh"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("executable bit lost: %v %v", info, err)
	}
	target, err := os.Readlink(filepath.Join(cloned, "link.go"))
	if err != nil || target != "tracked.go" {
		t.Fatalf("symlink lost: %q %v", target, err)
	}
	body, err := os.ReadFile(filepath.Join(cloned, "tracked.go"))
	if err != nil || !strings.Contains(string(body), "changed") {
		t.Fatalf("tracked modification missing: %s %v", body, err)
	}

	if err := git.ApplyTransferBundle(ctx, destination, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "notes.md")); err != nil {
		t.Fatal("sync-back did not apply untracked file")
	}
	if _, err := os.Stat(filepath.Join(destination, "ignored.txt")); !os.IsNotExist(err) {
		t.Fatal("ignored file appeared after apply")
	}
	if err := git.ApplyTransferBundle(ctx, destination, payload); err != nil {
		t.Fatal(err)
	}
	if err := git.ApplyTransferBundle(ctx, destination, nil); err != nil {
		t.Fatal(err)
	}
	if err := git.ApplyTransferBundle(ctx, destination, []byte("not a git bundle")); err == nil {
		t.Fatal("invalid sync bundle accepted")
	}
	if TransferChecksum(payload) == TransferChecksum(nil) {
		t.Fatal("checksum ignored payload")
	}
}

func TestTransferChecksumIsStableForPayload(t *testing.T) {
	if TransferChecksum([]byte("abc")) == TransferChecksum([]byte("abd")) {
		t.Fatal("distinct payloads produced the same checksum")
	}
	if TransferChecksum([]byte("abc")) != TransferChecksum([]byte("abc")) {
		t.Fatal("checksum was not stable")
	}
}

func TestApplyTransferBundleNoopsWhenHEADMatches(t *testing.T) {
	ctx := context.Background()
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	runTransferGit(t, source, "init", "-q")
	runTransferGit(t, source, "config", "user.email", "test@example.invalid")
	runTransferGit(t, source, "config", "user.name", "Agent Board Test")
	writeMode(t, filepath.Join(source, "README.md"), "same\n", 0o644)
	runTransferGit(t, source, "add", ".")
	runTransferGit(t, source, "commit", "-qm", "baseline")
	destination := cloneTransferBundle(t, mustTransferSnapshot(t, git, source, "match-1"))
	payload, err := git.TransferSnapshot(ctx, source, "match-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := git.ApplyTransferBundle(ctx, destination, payload); err != nil {
		t.Fatal(err)
	}
}

func mustTransferSnapshot(t *testing.T, git *GitCLI, source, id string) []byte {
	t.Helper()
	payload, err := git.TransferSnapshot(context.Background(), source, id)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestTransferSnapshotRequiresID(t *testing.T) {
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.TransferSnapshot(context.Background(), t.TempDir(), " "); err == nil {
		t.Fatal("blank transfer id accepted")
	}
}

func TestTransferOperationsSurfaceGitFailures(t *testing.T) {
	failing := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	git, err := NewGitCLI(failing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.TransferSnapshot(context.Background(), t.TempDir(), "xfer-fail"); err == nil {
		t.Fatal("failed snapshot git accepted")
	}
	if err := git.ApplyTransferBundle(context.Background(), t.TempDir(), []byte("bundle")); err == nil {
		t.Fatal("failed apply git accepted")
	}
	if nothingToCommit(nil) {
		t.Fatal("nil commit error treated as nothing to commit")
	}
	if nothingToCommit(os.ErrExist) {
		t.Fatal("unrelated error treated as nothing to commit")
	}
	if !nothingToCommit(errString("Nothing to commit")) {
		t.Fatal("nothing to commit not detected")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func cloneTransferBundle(t *testing.T, payload []byte) string {
	t.Helper()
	bundle := filepath.Join(t.TempDir(), "workspace.bundle")
	if err := os.WriteFile(bundle, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "clone")
	cmd := exec.Command("git", "clone", bundle, destination)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone bundle: %v\n%s", err, out)
	}
	return destination
}

func runTransferGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeMode(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
