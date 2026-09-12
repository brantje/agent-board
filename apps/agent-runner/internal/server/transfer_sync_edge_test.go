package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	runnerworkspace "github.com/brantje/agent-board/apps/agent-runner/internal/workspace"
)

func TestSyncWorkspaceBackRejectsNonRepository(t *testing.T) {
	runner, _ := newTestRunner(t)
	const sessionID = "non-repository"
	path := runner.manager.SessionWorkspacePath(sessionID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runner.transfers.markReady(sessionID, runnerworkspace.CheckoutState{Branch: "agent-board/AB-1", StartRevision: "missing"})
	writer := &recordingStreamWriter{}

	runner.syncWorkspaceBack(writer, sessionID, "")
	if len(writer.messageTypes) != 1 || writer.messageTypes[0] != protocol.TypeTransferFailed {
		t.Fatalf("messages=%v", writer.messageTypes)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unsafe workspace should be retained: %v", err)
	}
}

func TestSyncWorkspaceBackRejectsChangedBranchAndRetainsRepository(t *testing.T) {
	runner, _ := newTestRunner(t)
	const sessionID = "changed-branch"
	path := runner.manager.SessionWorkspacePath(sessionID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "agent-board/AB-1", path},
		{"-C", path, "config", "user.name", "Agent Board Test"},
		{"-C", path, "config", "user.email", "test@example.invalid"},
	} {
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(path, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	runGit("add", "-A")
	runGit("commit", "-q", "-m", "base")
	start := runnerTransferGitOutput(t, path, "rev-parse", "HEAD")
	runner.transfers.markReady(sessionID, runnerworkspace.CheckoutState{Branch: "agent-board/AB-1", StartRevision: start})
	runGit("checkout", "-q", "-b", "agent-board/other")
	writer := &recordingStreamWriter{}

	runner.syncWorkspaceBack(writer, sessionID, "sync-branch-change")
	if len(writer.messageTypes) != 1 || writer.messageTypes[0] != protocol.TypeTransferFailed {
		t.Fatalf("messages=%v", writer.messageTypes)
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		t.Fatalf("unsafe repository should be retained: %v", err)
	}
}

func runnerTransferGitOutput(t *testing.T, repositoryPath string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", repositoryPath}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for len(out) > 0 && (out[len(out)-1] == '\n' || out[len(out)-1] == '\r') {
		out = out[:len(out)-1]
	}
	return string(out)
}
