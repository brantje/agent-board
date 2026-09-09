package workspace

import (
	"context"
	"os/exec"
	"testing"
)

func TestCurrentBranchReportsDetachedHead(t *testing.T) {
	git, err := NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := git.InitRepository(context.Background(), repo, "main"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), "git", "-C", repo, "checkout", "--detach", "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("detach HEAD: %v: %s", err, out)
	}
	branch, err := git.CurrentBranch(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if branch == "" || branch[:4] != "HEAD" {
		t.Fatalf("branch=%q", branch)
	}
}
