package runexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func TestFinalizeServerWorkspaceRejectsDescendantOnWrongBranchWithoutPersisting(t *testing.T) {
	repo := initProcessTestRepository(t)
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	const issueBranch = "agent-board/AB-43"
	if err := git.CheckoutNewBranch(t.Context(), repo, issueBranch); err != nil {
		t.Fatal(err)
	}
	startRevision, err := git.HeadRevision(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}

	serverBranchGit(t, repo, "checkout", "-qb", "other-branch")
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("wrong branch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	serverBranchGit(t, repo, "add", "other.txt")
	serverBranchGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "wrong branch descendant")

	storeFake := &serverFinalizeStore{current: startRevision}
	processor := &Processor{store: storeFake, git: git}
	safe := processTestSafeContext(repo)
	safe.Workspace.WorkingBranch = issueBranch

	if _, err := processor.finalizeServerWorkspace(t.Context(), safe); err == nil {
		t.Fatal("wrong-branch descendant was finalized")
	}
	if storeFake.persisted != "" {
		t.Fatalf("wrong-branch finalization persisted revision %q", storeFake.persisted)
	}
}

func serverBranchGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
