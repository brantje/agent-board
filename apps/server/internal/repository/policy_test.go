package repository

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPolicyResolveAuthorizedCanonicalPath(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	got, err := policy.Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want, _ := filepath.EvalSymlinks(repo)
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestPolicyRejectsRelativeAndSiblingPrefixPaths(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repos")
	sibling := filepath.Join(parent, "repos-other")
	for _, path := range []string{root, sibling} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Resolve("../repo"); !errors.Is(err, ErrPathNotAbsolute) {
		t.Fatalf("relative path error = %v, want ErrPathNotAbsolute", err)
	}
	if _, err := policy.Resolve(sibling); !errors.Is(err, ErrPathNotAuthorized) {
		t.Fatalf("sibling path error = %v, want ErrPathNotAuthorized", err)
	}
}

func TestPolicyRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated Windows privileges")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "allowed")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	policy, _ := NewPolicy([]string{root})
	if _, err := policy.Resolve(link); !errors.Is(err, ErrPathNotAuthorized) {
		t.Fatalf("symlink escape error = %v, want ErrPathNotAuthorized", err)
	}
}

func TestPolicyRejectsMissingRootsAndNonDirectories(t *testing.T) {
	policy, _ := NewPolicy(nil)
	if _, err := policy.Resolve(t.TempDir()); !errors.Is(err, ErrNoAuthorizedRoots) {
		t.Fatalf("empty roots error = %v, want ErrNoAuthorizedRoots", err)
	}

	root := t.TempDir()
	file := filepath.Join(root, "repo.txt")
	if err := os.WriteFile(file, []byte("not a repo"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, _ = NewPolicy([]string{root})
	if _, err := policy.Resolve(file); !errors.Is(err, ErrPathNotDirectory) {
		t.Fatalf("file error = %v, want ErrPathNotDirectory", err)
	}
}

func TestParseRootsUsesPlatformPathListSeparator(t *testing.T) {
	joined := filepath.Join(t.TempDir(), "one") + string(os.PathListSeparator) + filepath.Join(t.TempDir(), "two")
	if got := ParseRoots(joined); len(got) != 2 {
		t.Fatalf("ParseRoots() len = %d, want 2", len(got))
	}
}
