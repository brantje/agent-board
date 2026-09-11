package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestRemoteGitTransferPreparesPublishesAndCleansAfterAck(t *testing.T) {
	origin, mainRevision := createRemoteTransferOrigin(t)
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	writer := &connectionWriter{conn: conn}

	prepare, err := json.Marshal(protocol.GitPrepare{
		CloneURL:    origin,
		Ref:         "main",
		IssueBranch: "agent-board/AB-77",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.sendTransfer(context.Background(), "remote-session", "git-prepare", protocol.TransferDirectionGitPrepare, prepare); err != nil {
		t.Fatal(err)
	}

	// Start immediately so this also proves the existing readiness fence waits
	// for remote Git preparation rather than introducing another lifecycle.
	send(t, conn, protocol.TypeStart, "remote-session", protocol.StartRequest{
		Command: []string{"sh", "-c", "printf remote-ok > remote.txt"},
		Dir:     "/workspace",
	})
	waitForExit(t, conn, "remote-session")
	waitFor(t, time.Second, func() bool { return runner.manager.ActiveCount() == 0 })

	worktree := runner.manager.SessionWorkspacePath("remote-session")
	if body, err := os.ReadFile(filepath.Join(worktree, "remote.txt")); err != nil || string(body) != "remote-ok" {
		t.Fatalf("remote execution output=%q err=%v", body, err)
	}
	if got := gitOutput(t, worktree, "rev-parse", "HEAD"); got != mainRevision {
		t.Fatalf("remote execution start=%q want=%q", got, mainRevision)
	}

	if err := writer.sendTransfer(context.Background(), "remote-session", "git-publish", protocol.TransferDirectionGitPublish, nil); err != nil {
		t.Fatal(err)
	}
	transferID, published := readGitPublishedTransfer(t, conn)
	if published.Revision == "" || published.Revision == mainRevision {
		t.Fatalf("published revision=%q start=%q", published.Revision, mainRevision)
	}
	if got := gitOutput(t, origin, "rev-parse", "refs/heads/agent-board/AB-77"); got != published.Revision {
		t.Fatalf("remote Issue branch=%q want=%q", got, published.Revision)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("remote worktree removed before durable acknowledgement: %v", err)
	}

	send(t, conn, protocol.TypeTransferApplied, "remote-session", protocol.TransferApplied{TransferID: transferID})
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(worktree)
		return os.IsNotExist(err)
	})
}

func TestRemoteGitPrepareFailureBlocksStart(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	writer := &connectionWriter{conn: conn}

	prepare, err := json.Marshal(protocol.GitPrepare{
		CloneURL:    filepath.Join(t.TempDir(), "missing.git"),
		IssueBranch: "agent-board/AB-78",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.sendTransfer(context.Background(), "remote-failed", "git-prepare", protocol.TransferDirectionGitPrepare, prepare); err != nil {
		t.Fatal(err)
	}
	failure := read(t, conn)
	if failure.Type != protocol.TypeError {
		t.Fatalf("prepare failure response=%+v", failure)
	}
	failurePayload, err := protocol.DecodePayload[protocol.ErrorPayload](failure)
	if err != nil || failurePayload.Code != "transfer_failed" {
		t.Fatalf("prepare failure=%+v err=%v", failurePayload, err)
	}

	send(t, conn, protocol.TypeStart, "remote-failed", protocol.StartRequest{Command: []string{"true"}, Dir: "/workspace"})
	startFailure := read(t, conn)
	if startFailure.Type != protocol.TypeError {
		t.Fatalf("start after failed prepare response=%+v", startFailure)
	}
	startPayload, err := protocol.DecodePayload[protocol.ErrorPayload](startFailure)
	if err != nil || startPayload.Code != "start_failed" {
		t.Fatalf("start after failed prepare=%+v err=%v", startPayload, err)
	}
}

func TestRemoteGitPublishFailureRetainsWorktree(t *testing.T) {
	origin, _ := createRemoteTransferOrigin(t)
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	writer := &connectionWriter{conn: conn}

	prepare, err := json.Marshal(protocol.GitPrepare{CloneURL: origin, IssueBranch: "agent-board/AB-79"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.sendTransfer(context.Background(), "remote-retain", "git-prepare", protocol.TransferDirectionGitPrepare, prepare); err != nil {
		t.Fatal(err)
	}
	send(t, conn, protocol.TypeStart, "remote-retain", protocol.StartRequest{Command: []string{"sh", "-c", "printf partial > partial.txt"}, Dir: "/workspace"})
	waitForExit(t, conn, "remote-retain")

	worktree := runner.manager.SessionWorkspacePath("remote-retain")
	moved := origin + ".unavailable"
	if err := os.Rename(origin, moved); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Rename(moved, origin) }()

	if err := writer.sendTransfer(context.Background(), "remote-retain", "git-publish", protocol.TransferDirectionGitPublish, nil); err != nil {
		t.Fatal(err)
	}
	failure := read(t, conn)
	if failure.Type != protocol.TypeTransferFailed {
		t.Fatalf("publish failure response=%+v", failure)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("failed publish removed recovery worktree: %v", err)
	}
	if got := gitOutput(t, worktree, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("failed publish did not preserve partial work as commit: %q", got)
	}
}

func readGitPublishedTransfer(t *testing.T, conn interface{ ReadJSON(any) error }) (string, protocol.GitPublished) {
	t.Helper()
	var transferID string
	var payload []byte
	for {
		var message protocol.Message
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		switch message.Type {
		case protocol.TypeTransferBegin:
			begin, err := protocol.DecodePayload[protocol.TransferBegin](message)
			if err != nil {
				t.Fatal(err)
			}
			transferID = begin.TransferID
		case protocol.TypeTransferChunk:
			chunk, err := protocol.DecodePayload[protocol.TransferChunk](message)
			if err != nil {
				t.Fatal(err)
			}
			data, err := base64.StdEncoding.DecodeString(chunk.Data)
			if err != nil {
				t.Fatal(err)
			}
			payload = append(payload, data...)
		case protocol.TypeTransferEnd:
			var published protocol.GitPublished
			if err := json.Unmarshal(payload, &published); err != nil {
				t.Fatal(err)
			}
			return transferID, published
		case protocol.TypeTransferFailed, protocol.TypeError:
			t.Fatalf("remote Git publication failed: %+v", message)
		}
	}
}

func createRemoteTransferOrigin(t *testing.T) (string, string) {
	t.Helper()
	working := filepath.Join(t.TempDir(), "working")
	runGit(t, "init", "-q", "-b", "main", working)
	if err := os.WriteFile(filepath.Join(working, "README.md"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", working, "add", "README.md")
	runGit(t, "-C", working, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "main")
	revision := gitOutput(t, working, "rev-parse", "HEAD")
	origin := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, "clone", "--bare", "-q", working, origin)
	return origin, revision
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", commandArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", commandArgs, err, out)
	}
	return strings.TrimSpace(string(out))
}
