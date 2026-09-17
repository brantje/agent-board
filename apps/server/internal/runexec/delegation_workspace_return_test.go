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
	continuation := filepath.Join(t.TempDir(), "parent-continuation")
	clone := exec.Command("git", "clone", "-q", repository, continuation)
	if output, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone pre-delegation parent Workspace: %v: %s", err, output)
	}

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
	if err := git.ApplyTransferBundle(t.Context(), continuation, payload); err != nil {
		t.Fatalf("apply delegated result through normal parent transfer: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(continuation, "returned.txt")); err != nil || string(body) != "from runner\n" {
		t.Fatalf("later parent Workspace returned.txt=%q err=%v", body, err)
	}
}
