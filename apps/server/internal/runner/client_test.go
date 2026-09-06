package runner

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func TestClientExecutesSessionAndPreservesStreams(t *testing.T) {
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}, func(conn *websocket.Conn, msg protocol.Message) {
		if msg.Type != protocol.TypeStart {
			return
		}
		writeProtocol(t, conn, protocol.TypeSessionStarted, msg.SessionID, nil)
		writeProtocol(t, conn, protocol.TypeStdout, msg.SessionID, protocol.StreamData{Data: []byte("hello")})
		writeProtocol(t, conn, protocol.TypeStderr, msg.SessionID, protocol.StreamData{Data: []byte("warning")})
		writeProtocol(t, conn, protocol.TypeExit, msg.SessionID, protocol.ExitResult{ExitCode: 7})
	})
	defer server.Close()

	conn, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	session, err := conn.Start(context.Background(), "session-1", Request{Command: []string{"sh", "-c", "exit 7"}, Dir: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(session.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(session.Stderr())
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(stdout) != "hello" || string(stderr) != "warning" || result.ExitCode != 7 {
		t.Fatalf("stdout=%q stderr=%q result=%+v", stdout, stderr, result)
	}
}

func TestClientForwardsStdinAndSignals(t *testing.T) {
	received := make(chan protocol.Message, 4)
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}, func(conn *websocket.Conn, msg protocol.Message) {
		received <- msg
		if msg.Type == protocol.TypeStart {
			writeProtocol(t, conn, protocol.TypeSessionStarted, msg.SessionID, nil)
		}
	})
	defer server.Close()
	conn, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	session, err := conn.Start(context.Background(), "session-2", Request{Command: []string{"cat"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Stdin().Write([]byte("input")); err != nil {
		t.Fatal(err)
	}
	if err := session.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Terminate(context.Background()); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(time.Second)
	seen := map[protocol.MessageType]bool{}
	for len(seen) < 4 {
		select {
		case msg := <-received:
			seen[msg.Type] = true
		case <-deadline:
			t.Fatalf("messages=%v", seen)
		}
	}
	for _, typ := range []protocol.MessageType{protocol.TypeStart, protocol.TypeStdin, protocol.TypeStdinClose, protocol.TypeTerminate} {
		if !seen[typ] {
			t.Fatalf("missing %s", typ)
		}
	}
}

func TestClientRejectsIncompatibleCapabilities(t *testing.T) {
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdout"}}, nil)
	defer server.Close()
	_, err := Dial(context.Background(), wsURL(server.URL))
	if !errors.Is(err, ErrIncompatibleRunner) {
		t.Fatalf("Dial() error=%v", err)
	}
}

func TestClientReportsDisconnectWithoutInventingExit(t *testing.T) {
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}, func(conn *websocket.Conn, msg protocol.Message) {
		if msg.Type == protocol.TypeStart {
			writeProtocol(t, conn, protocol.TypeSessionStarted, msg.SessionID, nil)
			_ = conn.Close()
		}
	})
	defer server.Close()
	conn, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	session, err := conn.Start(context.Background(), "session-3", Request{Command: []string{"sleep", "1"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Wait(context.Background())
	if !errors.Is(err, ErrDisconnected) {
		t.Fatalf("Wait() error=%v", err)
	}
}

func newProtocolTestServer(t *testing.T, caps protocol.Capabilities, handle func(*websocket.Conn, protocol.Message)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		hello, err := protocol.Decode(data)
		if err != nil || hello.Type != protocol.TypeServerHello {
			t.Errorf("server hello: msg=%+v err=%v", hello, err)
			return
		}
		writeProtocol(t, conn, protocol.TypeRunnerHello, "", protocol.RunnerHello{Version: protocol.Version1, Capabilities: caps})
		writeProtocol(t, conn, protocol.TypeHealth, "", protocol.Health{Status: "ok"})
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg, err := protocol.Decode(data)
			if err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			if handle != nil {
				handle(conn, msg)
			}
		}
	}))
}

func writeProtocol(t *testing.T, conn *websocket.Conn, typ protocol.MessageType, sessionID string, payload any) {
	t.Helper()
	msg, err := protocol.NewMessage(protocol.Version1, typ, sessionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	data, err := protocol.Encode(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatal(err)
	}
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}
