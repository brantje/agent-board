package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	"github.com/gorilla/websocket"
)

func TestSequentialSessionsReuseWorkspaceOverOneConnection(t *testing.T) {
	workspace := t.TempDir()
	runner := New(Config{WorkspaceRoot: workspace, MaxActiveSessions: 1})
	httpServer := httptest.NewServer(runner)
	defer httpServer.Close()
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "first", protocol.StartRequest{Command: []string{"sh", "-c", "printf persisted > state"}})
	waitForExit(t, conn, "first")
	waitFor(t, time.Second, func() bool { return runner.manager.ActiveCount() == 0 })

	send(t, conn, protocol.TypeStart, "second", protocol.StartRequest{Command: []string{"cat", "state"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted { t.Fatalf("unexpected %#v", msg) }
	var output string
	for {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil { t.Fatal(err) }
			output += string(stream.Data)
		case protocol.TypeExit:
			if output != "persisted" { t.Fatalf("unexpected workspace output %q", output) }
			if data, err := os.ReadFile(filepath.Join(workspace, "state")); err != nil || string(data) != "persisted" {
				t.Fatalf("workspace state mismatch data=%q err=%v", data, err)
			}
			return
		}
	}
}

func TestGracefulTerminateThenForcedKill(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "stubborn", protocol.StartRequest{Command: []string{"sh", "-c", "trap '' TERM; printf ready; sleep 30 & wait"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted { t.Fatalf("unexpected %#v", msg) }

	ready := false
	for !ready {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil { t.Fatal(err) }
			ready = strings.Contains(string(stream.Data), "ready")
		case protocol.TypeError:
			t.Fatalf("unexpected protocol error before readiness %#v", msg)
		case protocol.TypeExit:
			t.Fatalf("process exited before TERM trap was ready: %#v", msg)
		}
	}

	send(t, conn, protocol.TypeTerminate, "stubborn", nil)
	time.Sleep(100 * time.Millisecond)
	if runner.manager.ActiveCount() != 1 {
		t.Fatal("terminate unexpectedly removed a TERM-ignoring process")
	}
	send(t, conn, protocol.TypeKill, "stubborn", nil)
	for {
		msg := read(t, conn)
		if msg.Type != protocol.TypeExit { continue }
		result, err := protocol.DecodePayload[protocol.ExitResult](msg)
		if err != nil { t.Fatal(err) }
		if !result.Signaled { t.Fatalf("expected forced kill result %#v", result) }
		return
	}
}

func TestHealthRequestAndSessionNotFoundErrors(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeHealth, "", nil)
	if msg := read(t, conn); msg.Type != protocol.TypeHealth { t.Fatalf("unexpected %#v", msg) }
	send(t, conn, protocol.TypeKill, "missing", nil)
	assertProtocolError(t, read(t, conn), "session_not_found")
	send(t, conn, protocol.TypeStdin, "missing", protocol.StreamData{Data: []byte("x")})
	assertProtocolError(t, read(t, conn), "session_not_found")
	send(t, conn, protocol.TypeStdinClose, "missing", nil)
	assertProtocolError(t, read(t, conn), "session_not_found")
}

func TestMalformedPostHandshakeMessageFailsExplicitly(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"version":1,"type":"health"} trailing`)); err != nil { t.Fatal(err) }
	assertProtocolError(t, read(t, conn), "invalid_message")
}

func TestHandshakeRequiresTextServerHelloAndSupportedVersion(t *testing.T) {
	_, httpServer := newTestRunner(t)

	t.Run("binary", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(httpServer.URL), nil)
		if err != nil { t.Fatal(err) }
		defer conn.Close()
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte("hello")); err != nil { t.Fatal(err) }
		assertProtocolError(t, read(t, conn), "invalid_handshake")
	})

	t.Run("wrong type", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(httpServer.URL), nil)
		if err != nil { t.Fatal(err) }
		defer conn.Close()
		send(t, conn, protocol.TypeHealth, "", nil)
		assertProtocolError(t, read(t, conn), "invalid_handshake")
	})

	t.Run("no common version", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(httpServer.URL), nil)
		if err != nil { t.Fatal(err) }
		defer conn.Close()
		send(t, conn, protocol.TypeServerHello, "", protocol.ServerHello{SupportedVersions: []int{2}})
		assertProtocolError(t, read(t, conn), "unsupported_protocol_version")
	})
}

func waitForExit(t *testing.T, conn *websocket.Conn, sessionID string) {
	t.Helper()
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted || msg.SessionID != sessionID {
		t.Fatalf("unexpected start response %#v", msg)
	}
	for {
		msg := read(t, conn)
		if msg.Type == protocol.TypeExit && msg.SessionID == sessionID { return }
		if msg.Type == protocol.TypeError { t.Fatalf("unexpected error %#v", msg) }
	}
}
