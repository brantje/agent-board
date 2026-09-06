package evidence

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCandidateCollectorCapturesCompleteWorkspaceState(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	writeFile(t, root, "staged.txt", "base\n")
	writeFile(t, root, "unstaged.txt", "base\n")
	writeFile(t, root, "delete.txt", "delete\n")
	writeFile(t, root, "rename.txt", "rename\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")

	writeFile(t, root, "staged.txt", "changed staged\n")
	runGit(t, root, "add", "staged.txt")
	writeFile(t, root, "unstaged.txt", "changed unstaged\n")
	if err := os.Remove(filepath.Join(root, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "mv", "rename.txt", "renamed.txt")
	writeFile(t, root, "new.txt", "new\n")
	writeFile(t, root, "ignored.log", "ignored\n")
	writeFile(t, root, ".gitignore", "*.log\n")

	candidate, err := NewCandidateCollector().Collect(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := make(map[string]CandidateChange)
	for _, change := range candidate.Changes {
		byPath[change.Path] = change
	}
	if byPath["staged.txt"].StagedStatus != "modified" {
		t.Fatalf("missing staged modification: %+v", byPath)
	}
	if byPath["unstaged.txt"].UnstagedStatus != "modified" {
		t.Fatalf("missing unstaged modification: %+v", byPath)
	}
	if byPath["delete.txt"].UnstagedStatus != "deleted" {
		t.Fatalf("missing deletion: %+v", byPath)
	}
	if byPath["renamed.txt"].StagedStatus != "renamed" || byPath["renamed.txt"].OldPath != "rename.txt" {
		t.Fatalf("missing rename: %+v", byPath["renamed.txt"])
	}
	if !byPath["new.txt"].Untracked {
		t.Fatalf("missing untracked: %+v", byPath)
	}
	if _, exists := byPath["ignored.log"]; exists {
		t.Fatalf("ignored file included: %+v", byPath["ignored.log"])
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
