package repository

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPolicyEnsureCreatesMissingAuthorizedDirectory(t *testing.T) {
	root := t.TempDir()
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, "new-project")
	got, err := policy.Ensure(candidate)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	want, _ := filepath.EvalSymlinks(candidate)
	if got != want {
		t.Fatalf("Ensure() = %q, want %q", got, want)
	}
	info, err := os.Stat(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("Ensure() did not create a directory")
	}
}

func TestPolicyEnsureMakesReadOnlyDirectoryWritable(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "readonly")
	if err := os.Mkdir(repo, 0o555); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	got, err := policy.Ensure(repo)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if got != repo {
		t.Fatalf("Ensure() = %q, want %q", got, repo)
	}
	if err := os.WriteFile(filepath.Join(repo, "probe"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("directory still not writable: %v", err)
	}
}

func TestPolicyEnsureRejectsUnauthorizedPathsWithoutCreating(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repos")
	sibling := filepath.Join(parent, "outside")
	for _, path := range []string{root, sibling} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	missingOutside := filepath.Join(sibling, "new")
	if _, err := policy.Ensure(missingOutside); !errors.Is(err, ErrPathNotAuthorized) {
		t.Fatalf("Ensure() error = %v, want ErrPathNotAuthorized", err)
	}
	if _, err := os.Stat(missingOutside); !os.IsNotExist(err) {
		t.Fatal("Ensure() created a directory outside authorized roots")
	}
	if _, err := policy.Ensure("../repo"); !errors.Is(err, ErrPathNotAbsolute) {
		t.Fatalf("relative path error = %v, want ErrPathNotAbsolute", err)
	}
}

func TestPolicyEnsureRejectsSymlinkEscapeWithoutCreating(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated Windows privileges")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "allowed")
	outside := filepath.Join(parent, "outside")
	for _, path := range []string{root, outside} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	policy, _ := NewPolicy([]string{root})
	missing := filepath.Join(link, "nested")
	if _, err := policy.Ensure(missing); !errors.Is(err, ErrPathNotAuthorized) {
		t.Fatalf("symlink escape error = %v, want ErrPathNotAuthorized", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("Ensure() created a directory through a symlink escape")
	}
}
