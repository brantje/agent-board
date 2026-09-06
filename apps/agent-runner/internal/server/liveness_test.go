package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestStaleWebSocketIsReclaimedWithoutCancelingExecution(t *testing.T) {
	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	runner.pongWait = 250 * time.Millisecond
	runner.pingPeriod = 50 * time.Millisecond
	httpServer := httptest.NewServer(runner)
	defer httpServer.Close()

	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer func() { _ = conn.Close() }()
	send(t, conn, protocol.TypeStart, "liveness", protocol.StartRequest{Command: []string{"sh", "-c", "sleep 30"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", msg)
	}

	// Stop reading from the client. Gorilla only processes ping frames while a
	// reader is active, so this simulates a peer that can no longer service the
	// connection without explicitly closing its socket.
	waitFor(t, 2*time.Second, func() bool {
		runner.lifecycleMu.Lock()
		defer runner.lifecycleMu.Unlock()
		return len(runner.connections) == 0
	})
	if runner.manager.ActiveCount() != 1 {
		t.Fatalf("stale socket cleanup canceled active execution")
	}

	reconnected := dialAndHandshakeExpectHealth(t, httpServer.URL, 1, "liveness")
	defer func() { _ = reconnected.Close() }()
	send(t, reconnected, protocol.TypeKill, "liveness", nil)
	waitFor(t, 2*time.Second, func() bool { return runner.manager.ActiveCount() == 0 })
}

func TestReconnectReceivesRemainingOutputAndExit(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	send(t, conn, protocol.TypeStart, "reconnect", protocol.StartRequest{
		Command: []string{"sh", "-c", "read line; printf 'after:%s' \"$line\""},
	})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", msg)
	}
	_ = conn.Close()

	// Delivery ownership intentionally does not let a second socket steal an
	// active writer. Wait until the runner has observed the closed control
	// connection before establishing the replacement so this test exercises the
	// supported detach-then-reattach path rather than racing connection cleanup.
	waitFor(t, 2*time.Second, func() bool {
		runner.lifecycleMu.Lock()
		defer runner.lifecycleMu.Unlock()
		return len(runner.connections) == 0
	})

	reconnected := dialAndHandshakeExpectHealth(t, httpServer.URL, 1, "reconnect")
	defer func() { _ = reconnected.Close() }()
	send(t, reconnected, protocol.TypeStdin, "reconnect", protocol.StreamData{Data: []byte("hello\n")})
	send(t, reconnected, protocol.TypeStdinClose, "reconnect", nil)

	var stdout strings.Builder
	for {
		msg := read(t, reconnected)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil {
				t.Fatal(err)
			}
			stdout.Write(stream.Data)
		case protocol.TypeExit:
			result, err := protocol.DecodePayload[protocol.ExitResult](msg)
			if err != nil {
				t.Fatal(err)
			}
			if got := stdout.String(); got != "after:hello" {
				t.Fatalf("unexpected reattached stdout %q", got)
			}
			if result.ExitCode != 0 || result.Signaled {
				t.Fatalf("unexpected reconnect exit %#v", result)
			}
			return
		default:
			t.Fatalf("unexpected message after reconnect %#v", msg)
		}
	}
}
