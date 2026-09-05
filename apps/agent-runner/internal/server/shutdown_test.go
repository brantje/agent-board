package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestShutdownTerminatesActiveWebSocketSession(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "shutdown-session", protocol.StartRequest{
		Command: []string{"sh", "-c", "trap '' TERM; printf ready; while :; do sleep 1; done"},
	})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", msg)
	}

	var stdout strings.Builder
	for !strings.Contains(stdout.String(), "ready") {
		msg := read(t, conn)
		if msg.Type != protocol.TypeStdout {
			t.Fatalf("expected readiness output, got %#v", msg)
		}
		stream, err := protocol.DecodePayload[protocol.StreamData](msg)
		if err != nil {
			t.Fatal(err)
		}
		stdout.Write(stream.Data)
	}

	execution, err := runner.manager.Get("shutdown-session")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runner.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	if runner.manager.ActiveCount() != 0 {
		t.Fatalf("shutdown left %d active sessions", runner.manager.ActiveCount())
	}
	result, err := execution.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Signaled {
		t.Fatalf("expected TERM-ignoring session to be force-killed, got %#v", result)
	}

	_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected shutdown to close the WebSocket connection")
	}
}
