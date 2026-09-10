package server

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	runnerworkspace "github.com/brantje/agent-board/apps/agent-runner/internal/workspace"
)

func TestWorkspaceTransferExecutionAndCleanup(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	writer := &connectionWriter{conn: conn}
	if err := writer.sendTransfer(context.Background(), "session-1", "transfer-in", "to_runner", createGitBundlePayload(t)); err != nil {
		t.Fatal(err)
	}

	// Starting immediately after sending the bundle exercises the readiness
	// fence: execution must not observe a partially materialized Workspace.
	send(t, conn, protocol.TypeStart, "session-1", protocol.StartRequest{
		Command: []string{"sh", "-c", "cat README.md && printf changed > result.txt"},
		Dir:     "/workspace",
	})
	waitForExit(t, conn, "session-1")
	waitFor(t, time.Second, func() bool { return runner.manager.ActiveCount() == 0 })

	sessionRoot := runner.manager.SessionWorkspacePath("session-1")
	if _, err := os.Stat(filepath.Join(sessionRoot, "result.txt")); err != nil {
		t.Fatalf("execution did not use materialized Workspace: %v", err)
	}

	if err := writer.sendTransfer(context.Background(), "session-1", "transfer-out", "from_runner", nil); err != nil {
		t.Fatal(err)
	}
	var returnedTransferID string
	var receivedChunk bool
	for returnedTransferID == "" {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeTransferChunk:
			receivedChunk = true
		case protocol.TypeTransferEnd:
			end, err := protocol.DecodePayload[protocol.TransferEnd](msg)
			if err != nil {
				t.Fatal(err)
			}
			returnedTransferID = end.TransferID
		case protocol.TypeTransferFailed, protocol.TypeError:
			t.Fatalf("workspace sync failed: %#v", msg)
		}
	}
	if !receivedChunk {
		t.Fatal("workspace sync returned no payload")
	}
	if _, err := os.Stat(sessionRoot); err != nil {
		t.Fatalf("runner Workspace removed before apply acknowledgement: %v", err)
	}

	send(t, conn, protocol.TypeTransferApplied, "session-1", protocol.TransferApplied{TransferID: returnedTransferID})
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(sessionRoot)
		return os.IsNotExist(err)
	})
}

func TestIncomingTransferRejectsCorruptOrOversizedPayload(t *testing.T) {
	corrupt := newTransferState()
	if err := corrupt.begin("session-1", protocol.TransferBegin{
		TransferID: "transfer-1", Direction: "to_runner", TotalBytes: 4, Checksum: "wrong",
	}); err != nil {
		t.Fatal(err)
	}
	if err := corrupt.chunk("session-1", protocol.TransferChunk{
		TransferID: "transfer-1", Data: base64.StdEncoding.EncodeToString([]byte("data")),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := corrupt.end("session-1", protocol.TransferEnd{TransferID: "transfer-1"}); err == nil {
		t.Fatal("checksum mismatch accepted")
	}

	oversized := newTransferState()
	if err := oversized.begin("session-2", protocol.TransferBegin{
		TransferID: "transfer-2", Direction: "to_runner", TotalBytes: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := oversized.chunk("session-2", protocol.TransferChunk{
		TransferID: "transfer-2", Data: base64.StdEncoding.EncodeToString([]byte("too large")),
	}); err == nil {
		t.Fatal("payload larger than its declared size was accepted")
	}
}

func createGitBundlePayload(t *testing.T) []byte {
	t.Helper()
	source := t.TempDir()
	runGit(t, "-C", source, "init")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", source, "add", "README.md")
	runGit(t, "-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	payload, err := runnerworkspace.SnapshotBundle(context.Background(), source, "server-test-transfer")
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
