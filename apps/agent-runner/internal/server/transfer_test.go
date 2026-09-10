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

	send(t, conn, protocol.TypeTransferApplied, "session-1", protocol.TransferApplied{TransferID: "wrong-transfer"})
	invalidAck := read(t, conn)
	if invalidAck.Type != protocol.TypeError {
		t.Fatalf("invalid acknowledgement response=%+v", invalidAck)
	}
	invalidAckPayload, err := protocol.DecodePayload[protocol.ErrorPayload](invalidAck)
	if err != nil || invalidAckPayload.Code != "invalid_transfer_ack" {
		t.Fatalf("invalid acknowledgement error=%+v err=%v", invalidAckPayload, err)
	}
	if _, err := os.Stat(sessionRoot); err != nil {
		t.Fatalf("runner Workspace removed after invalid apply acknowledgement: %v", err)
	}

	send(t, conn, protocol.TypeTransferApplied, "session-1", protocol.TransferApplied{TransferID: returnedTransferID})
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(sessionRoot)
		return os.IsNotExist(err)
	})
}

func TestWorkspaceTransferFailureBlocksStartAndAllowsRetry(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	writer := &connectionWriter{conn: conn}

	if err := writer.sendTransfer(context.Background(), "session-retry", "broken-transfer", "to_runner", []byte("not a git bundle")); err != nil {
		t.Fatal(err)
	}
	failure := read(t, conn)
	if failure.Type != protocol.TypeError {
		t.Fatalf("invalid workspace transfer response=%+v", failure)
	}
	failurePayload, err := protocol.DecodePayload[protocol.ErrorPayload](failure)
	if err != nil || failurePayload.Code != "transfer_failed" {
		t.Fatalf("invalid workspace transfer error=%+v err=%v", failurePayload, err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	send(t, conn, protocol.TypeStart, "session-retry", protocol.StartRequest{Command: []string{"true"}, Dir: "/workspace"})
	startFailure := read(t, conn)
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if startFailure.Type != protocol.TypeError {
		t.Fatalf("start after invalid transfer response=%+v", startFailure)
	}
	startPayload, err := protocol.DecodePayload[protocol.ErrorPayload](startFailure)
	if err != nil || startPayload.Code != "start_failed" {
		t.Fatalf("start after invalid transfer error=%+v err=%v", startPayload, err)
	}

	if err := writer.sendTransfer(context.Background(), "session-retry", "retry-transfer", "to_runner", createGitBundlePayload(t)); err != nil {
		t.Fatal(err)
	}
	send(t, conn, protocol.TypeStart, "session-retry", protocol.StartRequest{Command: []string{"true"}, Dir: "/workspace"})
	waitForExit(t, conn, "session-retry")
}

func TestWorkspaceSyncFailsWithoutMaterializedRepository(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	writer := &connectionWriter{conn: conn}

	if err := writer.sendTransfer(context.Background(), "missing-workspace", "sync-missing", "from_runner", nil); err != nil {
		t.Fatal(err)
	}
	failed := read(t, conn)
	if failed.Type != protocol.TypeTransferFailed {
		t.Fatalf("missing workspace sync response=%+v", failed)
	}
	payload, err := protocol.DecodePayload[protocol.TransferFailed](failed)
	if err != nil || payload.TransferID != "sync-missing" || payload.Code != "transfer_failed" {
		t.Fatalf("missing workspace sync failure=%+v err=%v", payload, err)
	}
}

func TestIncomingTransferRejectsCorruptPayload(t *testing.T) {
	state := newTransferState()
	if err := state.begin("session-1", protocol.TransferBegin{
		TransferID: "transfer-1", Direction: "to_runner", TotalBytes: 4, Checksum: "wrong",
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.chunk("session-1", protocol.TransferChunk{
		TransferID: "transfer-1", Data: base64.StdEncoding.EncodeToString([]byte("data")),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := state.end("session-1", protocol.TransferEnd{TransferID: "transfer-1"}); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
}

func TestIncomingTransferRequiresSessionAndActiveState(t *testing.T) {
	state := newTransferState()
	if err := state.begin("", protocol.TransferBegin{TransferID: "transfer-1", Direction: "to_runner"}); err == nil {
		t.Fatal("transfer without session id was accepted")
	}
	if err := state.begin("session-1", protocol.TransferBegin{TransferID: "transfer-1", Direction: "sideways"}); err == nil {
		t.Fatal("transfer with invalid direction was accepted")
	}
	if err := state.chunk("session-1", protocol.TransferChunk{TransferID: "transfer-1"}); err == nil {
		t.Fatal("chunk without active transfer was accepted")
	}
	if _, _, err := state.end("session-1", protocol.TransferEnd{TransferID: "transfer-1"}); err == nil {
		t.Fatal("end without active transfer was accepted")
	}
}

func TestIncomingTransferIgnoresStaleTransferFrames(t *testing.T) {
	state := newTransferState()
	payload := []byte("current workspace")
	if err := state.begin("session-1", protocol.TransferBegin{
		TransferID: "current",
		Direction:  "to_runner",
		TotalBytes: int64(len(payload)),
		Checksum:   protocol.TransferChecksum(payload),
	}); err != nil {
		t.Fatal(err)
	}

	if err := state.chunk("session-1", protocol.TransferChunk{
		TransferID: "stale",
		Data:       base64.StdEncoding.EncodeToString([]byte("stale workspace")),
	}); err != nil {
		t.Fatalf("stale chunk should be ignored: %v", err)
	}
	if got, direction, err := state.end("session-1", protocol.TransferEnd{TransferID: "stale"}); err != nil || got != nil || direction != "" {
		t.Fatalf("stale end completed active transfer: payload=%q direction=%q err=%v", got, direction, err)
	}

	if err := state.chunk("session-1", protocol.TransferChunk{
		TransferID: "current",
		Data:       base64.StdEncoding.EncodeToString(payload),
	}); err != nil {
		t.Fatal(err)
	}
	got, direction, err := state.end("session-1", protocol.TransferEnd{TransferID: "current"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) || direction != "to_runner" {
		t.Fatalf("active transfer corrupted after stale frames: payload=%q direction=%q", got, direction)
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
