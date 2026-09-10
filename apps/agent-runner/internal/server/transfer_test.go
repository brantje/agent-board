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

func TestWorkspaceTransferMaterializesSessionDirectory(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	payload := createGitBundlePayload(t)
	writer := &connectionWriter{conn: conn}
	if err := writer.sendTransfer(context.Background(), "session-transfer", "transfer-1", "to_runner", payload); err != nil {
		t.Fatal(err)
	}

	sessionRoot := runner.manager.SessionWorkspacePath("session-transfer")
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(sessionRoot, "README.md"))
		return err == nil
	})

	send(t, conn, protocol.TypeStart, "session-transfer", protocol.StartRequest{
		Command: []string{"sh", "-c", "printf 'out:%s' \"$(cat README.md)\""},
		Dir:     "/workspace",
	})
	started := read(t, conn)
	if started.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", started)
	}

	var stdout string
	for {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil {
				t.Fatal(err)
			}
			stdout += string(stream.Data)
		case protocol.TypeExit:
			if stdout != "out:hello" {
				t.Fatalf("unexpected stdout %q", stdout)
			}
			return
		case protocol.TypeError:
			t.Fatalf("unexpected error %#v", msg)
		}
	}
}

func TestStartWaitsForInFlightWorkspaceTransfer(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	payload := createGitBundlePayload(t)
	writer := &connectionWriter{conn: conn}
	if err := writer.sendTransfer(context.Background(), "session-wait", "transfer-wait", "to_runner", payload); err != nil {
		t.Fatal(err)
	}
	send(t, conn, protocol.TypeStart, "session-wait", protocol.StartRequest{
		Command: []string{"sh", "-c", "test -f README.md && printf ready"},
		Dir:     "/workspace",
	})
	started := read(t, conn)
	if started.Type != protocol.TypeSessionStarted {
		t.Fatalf("start before materialize finished: %#v", started)
	}
	var stdout string
	for {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil {
				t.Fatal(err)
			}
			stdout += string(stream.Data)
		case protocol.TypeExit:
			if stdout != "ready" {
				t.Fatalf("workspace was not ready for start, stdout=%q", stdout)
			}
			return
		case protocol.TypeError:
			t.Fatalf("unexpected error %#v", msg)
		}
	}
}

func TestWorkspaceSnapshotCleansSessionOnlyAfterAppliedAck(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	payload := createGitBundlePayload(t)
	writer := &connectionWriter{conn: conn}
	if err := writer.sendTransfer(context.Background(), "session-sync", "transfer-out", "to_runner", payload); err != nil {
		t.Fatal(err)
	}
	sessionRoot := runner.manager.SessionWorkspacePath("session-sync")
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(sessionRoot, "README.md"))
		return err == nil
	})
	writeFile(t, filepath.Join(sessionRoot, "untracked.txt"), "created-on-runner\n")

	if err := writer.sendTransfer(context.Background(), "session-sync", "transfer-back", "from_runner", nil); err != nil {
		t.Fatal(err)
	}
	var snapshot []byte
	var returnedTransferID string
	for returnedTransferID == "" {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeTransferBegin:
			begin, err := protocol.DecodePayload[protocol.TransferBegin](msg)
			if err != nil {
				t.Fatal(err)
			}
			if begin.Direction != "from_runner" || begin.TotalBytes <= 0 {
				t.Fatalf("unexpected snapshot begin %#v", begin)
			}
		case protocol.TypeTransferChunk:
			chunk, err := protocol.DecodePayload[protocol.TransferChunk](msg)
			if err != nil {
				t.Fatal(err)
			}
			data, err := base64.StdEncoding.DecodeString(chunk.Data)
			if err != nil {
				t.Fatal(err)
			}
			snapshot = append(snapshot, data...)
		case protocol.TypeTransferEnd:
			end, err := protocol.DecodePayload[protocol.TransferEnd](msg)
			if err != nil {
				t.Fatal(err)
			}
			returnedTransferID = end.TransferID
		case protocol.TypeTransferFailed:
			t.Fatalf("snapshot failed %#v", msg)
		case protocol.TypeError:
			t.Fatalf("unexpected error %#v", msg)
		}
	}
	if len(snapshot) == 0 {
		t.Fatal("empty workspace snapshot")
	}
	if _, err := os.Stat(sessionRoot); err != nil {
		t.Fatalf("runner workspace removed before apply acknowledgement: %v", err)
	}

	send(t, conn, protocol.TypeTransferApplied, "session-sync", protocol.TransferApplied{TransferID: returnedTransferID})
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(sessionRoot)
		return os.IsNotExist(err)
	})
}

func TestWorkspaceSnapshotPreservedOnWrongAckAndDisconnect(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)

	writer := &connectionWriter{conn: conn}
	if err := writer.sendTransfer(context.Background(), "session-preserve", "transfer-out", "to_runner", createGitBundlePayload(t)); err != nil {
		t.Fatal(err)
	}
	sessionRoot := runner.manager.SessionWorkspacePath("session-preserve")
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(sessionRoot, "README.md"))
		return err == nil
	})
	writeFile(t, filepath.Join(sessionRoot, "recoverable.txt"), "keep me\n")
	if err := writer.sendTransfer(context.Background(), "session-preserve", "transfer-back", "from_runner", nil); err != nil {
		t.Fatal(err)
	}
	var returnedTransferID string
	for returnedTransferID == "" {
		msg := read(t, conn)
		if msg.Type == protocol.TypeTransferEnd {
			end, err := protocol.DecodePayload[protocol.TransferEnd](msg)
			if err != nil {
				t.Fatal(err)
			}
			returnedTransferID = end.TransferID
		} else if msg.Type == protocol.TypeError || msg.Type == protocol.TypeTransferFailed {
			t.Fatalf("unexpected transfer failure %#v", msg)
		}
	}

	send(t, conn, protocol.TypeTransferApplied, "session-preserve", protocol.TransferApplied{TransferID: returnedTransferID + "-wrong"})
	assertProtocolError(t, read(t, conn), "invalid_transfer_ack")
	if _, err := os.Stat(filepath.Join(sessionRoot, "recoverable.txt")); err != nil {
		t.Fatalf("wrong acknowledgement removed runner workspace: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(sessionRoot, "recoverable.txt")); err != nil {
		t.Fatalf("disconnect before apply acknowledgement removed runner workspace: %v", err)
	}
}

func TestSequentialStartsOnSameSessionReuseWorkspace(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	payload := createGitBundlePayload(t)
	writer := &connectionWriter{conn: conn}
	if err := writer.sendTransfer(context.Background(), "session-reuse", "transfer-1", "to_runner", payload); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(runner.manager.SessionWorkspacePath("session-reuse"), "README.md"))
		return err == nil
	})

	send(t, conn, protocol.TypeStart, "session-reuse", protocol.StartRequest{
		Command: []string{"sh", "-c", "printf marker > created.txt"},
		Dir:     "/workspace",
	})
	waitForExit(t, conn, "session-reuse")
	waitFor(t, time.Second, func() bool { return runner.manager.ActiveCount() == 0 })
	if _, err := os.Stat(filepath.Join(runner.manager.SessionWorkspacePath("session-reuse"), "created.txt")); err != nil {
		t.Fatalf("workspace was cleaned before engine-level sync: %v", err)
	}

	send(t, conn, protocol.TypeStart, "session-reuse", protocol.StartRequest{
		Command: []string{"sh", "-c", "cat created.txt"},
		Dir:     "/workspace",
	})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected %#v", msg)
	}
	var output string
	for {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil {
				t.Fatal(err)
			}
			output += string(stream.Data)
		case protocol.TypeExit:
			if output != "marker" {
				t.Fatalf("sequential process did not reuse workspace, stdout=%q", output)
			}
			return
		case protocol.TypeError:
			t.Fatalf("unexpected error %#v", msg)
		}
	}
}

func TestWorkspaceTransferFailsOnInvalidBundle(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	writer := &connectionWriter{conn: conn}
	if err := writer.sendTransfer(context.Background(), "session-bad", "transfer-bad", "to_runner", []byte("not a git bundle")); err != nil {
		t.Fatal(err)
	}
	msg := read(t, conn)
	if msg.Type != protocol.TypeError {
		t.Fatalf("expected transfer error, got %#v", msg)
	}
}

func TestTransferHandlersRejectInvalidPayloadsAndFailedStart(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeTransferBegin, "session-invalid", "nope")
	assertProtocolError(t, read(t, conn), "invalid_transfer")
	send(t, conn, protocol.TypeTransferChunk, "session-invalid", "nope")
	assertProtocolError(t, read(t, conn), "invalid_transfer")
	send(t, conn, protocol.TypeTransferEnd, "session-invalid", "nope")
	assertProtocolError(t, read(t, conn), "invalid_transfer")
	send(t, conn, protocol.TypeTransferApplied, "session-invalid", "nope")
	assertProtocolError(t, read(t, conn), "invalid_transfer_ack")

	send(t, conn, protocol.TypeTransferBegin, "session-invalid", protocol.TransferBegin{TotalBytes: 1})
	assertProtocolError(t, read(t, conn), "transfer_failed")

	writer := &connectionWriter{conn: conn}
	if err := writer.sendTransfer(context.Background(), "session-failed", "transfer-failed", "to_runner", []byte("not a git bundle")); err != nil {
		t.Fatal(err)
	}
	assertProtocolError(t, read(t, conn), "transfer_failed")
	send(t, conn, protocol.TypeStart, "session-failed", protocol.StartRequest{Command: []string{"true"}})
	assertProtocolError(t, read(t, conn), "start_failed")
}

func TestSyncWorkspaceBackFailsWithoutUsableRepository(t *testing.T) {
	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	writer := &recordingStreamWriter{}
	runner.syncWorkspaceBack(writer, "../escape", "t1")
	if len(writer.messageTypes) != 1 || writer.messageTypes[0] != protocol.TypeTransferFailed {
		t.Fatalf("expected transfer failed for invalid session, got %#v", writer.messageTypes)
	}

	emptyID := "session-empty"
	if err := os.MkdirAll(runner.manager.SessionWorkspacePath(emptyID), 0o755); err != nil {
		t.Fatal(err)
	}
	emptyWriter := &recordingStreamWriter{}
	runner.syncWorkspaceBack(emptyWriter, emptyID, "")
	if len(emptyWriter.messageTypes) != 1 || emptyWriter.messageTypes[0] != protocol.TypeTransferFailed {
		t.Fatalf("expected transfer failed for non-repository, got %#v", emptyWriter.messageTypes)
	}
}

func createGitBundlePayload(t *testing.T) []byte {
	t.Helper()
	source := t.TempDir()
	runGit(t, "-C", source, "init")
	writeFile(t, filepath.Join(source, "README.md"), "hello\n")
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

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
