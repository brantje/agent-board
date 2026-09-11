package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFinalizeCheckoutCommitsLeftoversAndPreservesStartRevision(t *testing.T) {
	git := requireGit(t)
	repo := createFixtureRepository(t, git.GitCLI, t.TempDir())
	const branch = "agent-board/AB-1"
	if err := git.CheckoutNewBranch(t.Context(), repo, branch); err != nil {
		t.Fatal(err)
	}
	startRevision, err := git.HeadRevision(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	revision, err := git.FinalizeCheckout(t.Context(), repo, branch, startRevision)
	if err != nil {
		t.Fatalf("FinalizeCheckout() error=%v", err)
	}
	if revision == startRevision {
		t.Fatal("FinalizeCheckout() did not commit leftover changes")
	}
	if head, err := git.HeadRevision(t.Context(), repo); err != nil || head != revision {
		t.Fatalf("HEAD=%q err=%v want %q", head, err, revision)
	}

	runGit(t, git.binary, "-C", repo, "merge-base", "--is-ancestor", startRevision, revision)
}
