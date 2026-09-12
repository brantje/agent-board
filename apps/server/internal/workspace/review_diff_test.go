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

func stringsRepeat(value string, count int) string {
	out := make([]byte, 0, len(value)*count)
	for range count {
		out = append(out, value...)
	}
	return string(out)
}
