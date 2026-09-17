package runexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func TestDelegatedWorkspaceResultFeedsLaterNormalTransfer(t *testing.T) {
	repository := initProcessTestRepository(t)
	safe := processTestSafeContext(repository)
	safe.Runner = &executioncontext.RunnerContext{ID: "runner-1"}
	baseStore := &runnerSyncStore{}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, repository)}
	processor := newRunnerSyncProcessor(t, repository, safe, baseStore, client)
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

	result, err := processor.runEngineOnRunner(t.Context(), run, safe, "runner-1", "delegate-session")
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "READY_FOR_REVIEW" {
		t.Fatalf("delegated result=%+v", result)
	}

	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := git.TransferSnapshot(t.Context(), repository, "later-parent-transfer")
	if err != nil {
		t.Fatal(err)
	}
	assertDelegationTransferContainsFile(t, payload, "returned.txt")
}

func assertDelegationTransferContainsFile(t *testing.T, payload []byte, name string) {
	t.Helper()
	root := t.TempDir()
	bundlePath := filepath.Join(root, "workspace.bundle")
	if err := os.WriteFile(bundlePath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	checkoutPath := filepath.Join(root, "checkout")
	command := exec.Command("git", "clone", "-q", bundlePath, checkoutPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("clone later parent transfer: %v: %s", err, output)
	}
	if body, err := os.ReadFile(filepath.Join(checkoutPath, name)); err != nil || string(body) != "from runner\n" {
		t.Fatalf("later parent transfer file %s=%q err=%v", name, body, err)
	}
}
