package server

import (
	"net/http/httptest"
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
