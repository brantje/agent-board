package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReviewFileChangesSummarizesDiffStats(t *testing.T) {
	repo := t.TempDir()
	git, err := NewGitCLI("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := git.InitRepository(t.Context(), repo, "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "index.html"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(t.Context(), "-C", repo, "add", "index.html"); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(t.Context(), "-C", repo, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	baseRevision, err := git.HeadRevision(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "index.html"), []byte(stringsRepeat("line\n", 899)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(t.Context(), "-C", repo, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(t.Context(), "-C", repo, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "review"); err != nil {
		t.Fatal(err)
	}
	reviewRevision, err := git.HeadRevision(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}

	changes, err := git.ReviewFileChanges(t.Context(), repo, baseRevision, reviewRevision)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes=%+v", changes)
	}
	byPath := map[string]ReviewFileChange{}
	for _, change := range changes {
		byPath[change.Path] = change
	}
	if byPath["index.html"].ChangeType != "modified" || byPath["index.html"].Added != 899 || byPath["index.html"].Removed != 1 {
		t.Fatalf("index.html=%+v", byPath["index.html"])
	}
	if byPath["new.txt"].ChangeType != "created" || byPath["new.txt"].Added != 2 || byPath["new.txt"].Removed != 0 {
		t.Fatalf("new.txt=%+v", byPath["new.txt"])
	}
}

func TestReviewFileChangesRejectsMissingRevisions(t *testing.T) {
	git, err := NewGitCLI("git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.ReviewFileChanges(t.Context(), t.TempDir(), "", "abc"); err == nil {
		t.Fatal("expected missing revisions to fail")
	}
}

func TestReviewFileChangesSameRevisionIsEmpty(t *testing.T) {
	git, err := NewGitCLI("git")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := git.ReviewFileChanges(t.Context(), t.TempDir(), "abc", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes=%+v", changes)
	}
}

func TestParseDiffHelpersCoverRenameDeleteAndBinaryStats(t *testing.T) {
	stats := parseDiffNumstat("1\t2\tmodified.txt\n-\t-\tbinary.bin\n")
	if stats["modified.txt"].Added != 1 || stats["modified.txt"].Removed != 2 {
		t.Fatalf("modified stats=%+v", stats["modified.txt"])
	}
	if stats["binary.bin"].Added != 0 || stats["binary.bin"].Removed != 0 {
		t.Fatalf("binary stats=%+v", stats["binary.bin"])
	}

	changes := parseDiffNameStatus("M\tmodified.txt\nA\tcreated.txt\nD\tdeleted.txt\nR100\told.txt\tnew.txt\n", stats)
	byPath := map[string]ReviewFileChange{}
	for _, change := range changes {
		byPath[change.Path] = change
	}
	if byPath["modified.txt"].ChangeType != "modified" || byPath["created.txt"].ChangeType != "created" || byPath["deleted.txt"].ChangeType != "deleted" {
		t.Fatalf("changes=%+v", byPath)
	}
	if byPath["new.txt"].ChangeType != "renamed" || byPath["new.txt"].OldPath != "old.txt" {
		t.Fatalf("rename=%+v", byPath["new.txt"])
	}
}

func stringsRepeat(value string, count int) string {
	out := make([]byte, 0, len(value)*count)
	for range count {
		out = append(out, value...)
	}
	return string(out)
}
