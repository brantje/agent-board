package repository

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyRejectsRetargetedAuthorizedRoot(t *testing.T) {
	parent := t.TempDir()
	authorized := filepath.Join(parent, "authorized")
	outside := filepath.Join(parent, "outside")
	candidate := filepath.Join(outside, "repository")
	for _, path := range []string{authorized, candidate} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := NewPolicy([]string{authorized})
	if err != nil {
		t.Fatal(err)
	}
	original := authorized + "-original"
	if err := os.Rename(authorized, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, authorized); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := policy.Resolve(candidate); !errors.Is(err, ErrPathNotAuthorized) {
		t.Fatalf("retargeted authorized root error = %v, want ErrPathNotAuthorized", err)
	}
}
