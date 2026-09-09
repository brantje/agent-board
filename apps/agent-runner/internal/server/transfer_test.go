package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestWorkspaceTransferMaterializesSessionDirectory(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	source := t.TempDir()
	runGit(t, "-C", source, "init")
	writeFile(t, filepath.Join(source, "README.md"), "hello\n")
	runGit(t, "-C", source, "add", "README.md")
	runGit(t, "-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")

	bundlePath := filepath.Join(t.TempDir(), "workspace.bundle")
	runGit(t, "-C", source, "bundle", "create", bundlePath, "HEAD")
	payload, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}

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
