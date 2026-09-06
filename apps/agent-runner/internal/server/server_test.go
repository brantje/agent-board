package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	"github.com/gorilla/websocket"
)

func TestHealthEndpoint(t *testing.T) {
	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	httpServer := httptest.NewServer(runner)
	defer httpServer.Close()

	response, err := http.Get(httpServer.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", response.StatusCode)
	}
	var health protocol.Health
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "ok" || health.ActiveSessions != 0 {
		t.Fatalf("unexpected health %#v", health)
	}
}

func TestWebSocketExecutionLifecycle(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "session-1", protocol.StartRequest{
		Command: []string{"sh", "-c", "read line; printf 'out:%s' \"$line\"; printf 'err:%s' \"$TOKEN\" >&2; exit 7"},
		Env: map[string]string{"TOKEN": "env"}, Secrets: map[string]string{"TOKEN": "secret"},
	})
	started := read(t, conn)
	if started.Type != protocol.TypeSessionStarted || started.SessionID != "session-1" {
		t.Fatalf("unexpected start response %#v", started)
	}
	send(t, conn, protocol.TypeStdin, "session-1", protocol.StreamData{Data: []byte("hello\n")})
	send(t, conn, protocol.TypeStdinClose, "session-1", nil)

	var stdout, stderr string
	var exit protocol.ExitResult
	for {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil { t.Fatal(err) }
			stdout += string(stream.Data)
		case protocol.TypeStderr:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil { t.Fatal(err) }
			stderr += string(stream.Data)
		case protocol.TypeExit:
			var err error
			exit, err = protocol.DecodePayload[protocol.ExitResult](msg)
			if err != nil { t.Fatal(err) }
			if stdout != "out:hello" || stderr != "err:***" || exit.ExitCode != 7 || exit.Signaled {
				t.Fatalf("unexpected lifecycle stdout=%q stderr=%q exit=%#v", stdout, stderr, exit)
			}
			return
		default:
			t.Fatalf("unexpected message %#v", msg)
		}
	}
}

func TestCapacityAndKill(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "first", protocol.StartRequest{Command: []string{"sh", "-c", "sleep 30"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted { t.Fatalf("unexpected %#v", msg) }
	send(t, conn, protocol.TypeStart, "second", protocol.StartRequest{Command: []string{"true"}})
	errMsg := read(t, conn)
	assertProtocolError(t, errMsg, "capacity_reached")
	send(t, conn, protocol.TypeKill, "first", nil)
	for {
		msg := read(t, conn)
		if msg.Type == protocol.TypeExit {
			result, err := protocol.DecodePayload[protocol.ExitResult](msg)
			if err != nil { t.Fatal(err) }
			if !result.Signaled { t.Fatalf("expected signaled exit %#v", result) }
			return
		}
	}
}

func TestDisconnectPreservesExecutionAndReconnectReportsIt(t *testing.T) {
	runner, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	send(t, conn, protocol.TypeStart, "survivor", protocol.StartRequest{Command: []string{"sh", "-c", "sleep 30"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted { t.Fatalf("unexpected %#v", msg) }
	_ = conn.Close()

	reconnected := dialAndHandshakeExpectHealth(t, httpServer.URL, 1, "survivor")
	defer reconnected.Close()
	send(t, reconnected, protocol.TypeKill, "survivor", nil)
	waitFor(t, 2*time.Second, func() bool { return runner.manager.ActiveCount() == 0 })
}

func TestDisconnectDoesNotCancelProcess(t *testing.T) {
	workspace := t.TempDir()
	runner := New(Config{WorkspaceRoot: workspace, MaxActiveSessions: 1})
	httpServer := httptest.NewServer(runner)
	defer httpServer.Close()
	conn := dialAndHandshake(t, httpServer.URL, 1)
	send(t, conn, protocol.TypeStart, "detached", protocol.StartRequest{Command: []string{"sh", "-c", "sleep 0.1; printf done > state"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted { t.Fatalf("unexpected %#v", msg) }
	_ = conn.Close()
	statePath := filepath.Join(workspace, "state")
	waitFor(t, 2*time.Second, func() bool {
		data, err := os.ReadFile(statePath)
		return err == nil && string(data) == "done"
	})
	waitFor(t, 2*time.Second, func() bool { return runner.manager.ActiveCount() == 0 })
}

func TestUnsupportedProtocolVersionFailsExplicitly(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(httpServer.URL), nil)
	if err != nil { t.Fatal(err) }
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"version":2,"type":"server_hello","payload":{"supported_versions":[2]}}`)); err != nil { t.Fatal(err) }
	msg := read(t, conn)
	assertProtocolError(t, msg, "unsupported_protocol_version")
}

func TestInvalidMessagesAndSecretsAreNotReflected(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("binary")); err != nil { t.Fatal(err) }
	assertProtocolError(t, read(t, conn), "invalid_message")

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"version":1,"type":"start","session_id":"bad","payload":{"command":"not-an-array"}}`)); err != nil { t.Fatal(err) }
	assertProtocolError(t, read(t, conn), "invalid_start")

	secret := "do-not-reflect-this-secret"
	send(t, conn, protocol.TypeStart, "secret", protocol.StartRequest{Command: []string{"/definitely/missing/command"}, Secrets: map[string]string{"TOKEN": secret}})
	msg := read(t, conn)
	assertProtocolError(t, msg, "start_failed")
	encoded, _ := json.Marshal(msg)
	if strings.Contains(string(encoded), secret) { t.Fatalf("secret reflected in protocol response: %s", encoded) }

	send(t, conn, protocol.TypeRunnerHello, "", protocol.RunnerHello{Version: protocol.Version1})
	assertProtocolError(t, read(t, conn), "invalid_direction")
}

func TestBrowserOriginIsRejected(t *testing.T) {
	_, httpServer := newTestRunner(t)
	header := http.Header{"Origin": []string{"https://example.invalid"}}
	conn, response, err := websocket.DefaultDialer.Dial(wsURL(httpServer.URL), header)
	if conn != nil { conn.Close() }
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden origin, response=%v err=%v", response, err)
	}
}

func newTestRunner(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	httpServer := httptest.NewServer(runner)
	t.Cleanup(httpServer.Close)
	return runner, httpServer
}

func dialAndHandshake(t *testing.T, baseURL string, capacity int) *websocket.Conn {
	t.Helper()
	return dialAndHandshakeExpectHealth(t, baseURL, capacity, "")
}

func dialAndHandshakeExpectHealth(t *testing.T, baseURL string, capacity int, activeID string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(baseURL), nil)
	if err != nil { t.Fatal(err) }
	send(t, conn, protocol.TypeServerHello, "", protocol.ServerHello{SupportedVersions: []int{protocol.Version1}})
	helloMsg := read(t, conn)
	if helloMsg.Type != protocol.TypeRunnerHello { t.Fatalf("expected runner hello, got %#v", helloMsg) }
	hello, err := protocol.DecodePayload[protocol.RunnerHello](helloMsg)
	if err != nil { t.Fatal(err) }
	if hello.Version != protocol.Version1 || hello.Capabilities.MaxActiveSessions != capacity {
		t.Fatalf("unexpected runner hello %#v", hello)
	}
	healthMsg := read(t, conn)
	if healthMsg.Type != protocol.TypeHealth { t.Fatalf("expected health, got %#v", healthMsg) }
	health, err := protocol.DecodePayload[protocol.Health](healthMsg)
	if err != nil { t.Fatal(err) }
	if activeID != "" {
		if len(health.ActiveSessionIDs) != 1 || health.ActiveSessionIDs[0] != activeID {
			t.Fatalf("expected active session %q, health=%#v", activeID, health)
		}
	}
	return conn
}

func send(t *testing.T, conn *websocket.Conn, typ protocol.MessageType, sessionID string, payload any) {
	t.Helper()
	msg, err := protocol.NewMessage(protocol.Version1, typ, sessionID, payload)
	if err != nil { t.Fatal(err) }
	data, err := protocol.Encode(msg)
	if err != nil { t.Fatal(err) }
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil { t.Fatal(err) }
}

func read(t *testing.T, conn *websocket.Conn) protocol.Message {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	kind, data, err := conn.ReadMessage()
	if err != nil { t.Fatal(err) }
	if kind != websocket.TextMessage { t.Fatalf("expected text message, got %d", kind) }
	msg, err := protocol.Decode(data)
	if err != nil { t.Fatal(err) }
	return msg
}

func assertProtocolError(t *testing.T, msg protocol.Message, code string) {
	t.Helper()
	if msg.Type != protocol.TypeError { t.Fatalf("expected error, got %#v", msg) }
	payload, err := protocol.DecodePayload[protocol.ErrorPayload](msg)
	if err != nil { t.Fatal(err) }
	if payload.Code != code { t.Fatalf("expected error code %q, got %#v", code, payload) }
}

func wsURL(httpURL string) string { return "ws" + strings.TrimPrefix(httpURL, "http") + "/v1/ws" }

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() { return }
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("condition not reached within %s", timeout))
}
