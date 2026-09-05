package repository

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewPolicyRejectsRelativeAuthorizedRoot(t *testing.T) {
	if _, err := NewPolicy([]string{"relative/repos"}); !errors.Is(err, ErrPathNotAbsolute) {
		t.Fatalf("NewPolicy() error = %v, want ErrPathNotAbsolute", err)
	}
}

func TestParseRootsHandlesEmptyAndWhitespaceEntries(t *testing.T) {
	if got := ParseRoots("   "); got != nil {
		t.Fatalf("ParseRoots(blank) = %#v, want nil", got)
	}
	root := t.TempDir()
	value := " " + root + " " + string(os.PathListSeparator) + "   "
	got := ParseRoots(value)
	if len(got) != 1 || got[0] != root {
		t.Fatalf("ParseRoots() = %#v, want [%q]", got, root)
	}
}

func TestPolicyRejectsUnavailableRepositoryAndUnavailableRoot(t *testing.T) {
	root := t.TempDir()
	policy, err := NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing")
	if _, err := policy.Resolve(missing); !errors.Is(err, ErrPathUnavailable) {
		t.Fatalf("missing repository error = %v, want ErrPathUnavailable", err)
	}

	unavailableRoot := filepath.Join(t.TempDir(), "missing-root")
	if _, err := NewPolicy([]string{unavailableRoot}); !errors.Is(err, ErrPathUnavailable) {
		t.Fatalf("unavailable root error = %v, want ErrPathUnavailable", err)
	}

	rootFile := filepath.Join(t.TempDir(), "root.txt")
	if err := os.WriteFile(rootFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPolicy([]string{rootFile}); !errors.Is(err, ErrPathNotDirectory) {
		t.Fatalf("non-directory root error = %v, want ErrPathNotDirectory", err)
	}

	transientRoot := t.TempDir()
	policy, err = NewPolicy([]string{transientRoot})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(transientRoot); err != nil {
		t.Fatal(err)
	}
	candidate := t.TempDir()
	if _, err := policy.Resolve(candidate); !errors.Is(err, ErrPathNotAuthorized) {
		t.Fatalf("root unavailable after startup error = %v, want ErrPathNotAuthorized", err)
	}
}

func TestWithinRejectsParentAndAcceptsRoot(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	if !within(root, root) {
		t.Fatal("root should be within itself")
	}
	if within(root, filepath.Dir(root)) {
		t.Fatal("parent directory must not be within root")
	}
}
