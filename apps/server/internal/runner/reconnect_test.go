package runner

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func TestAttachReplaysTerminalDeliveryThatArrivedBeforeRegistration(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		caps := protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}
		writeProtocol(t, conn, protocol.TypeRunnerHello, "", protocol.RunnerHello{Version: protocol.Version1, Capabilities: caps})
		writeProtocol(t, conn, protocol.TypeHealth, "", protocol.Health{Status: "ok"})
		writeProtocol(t, conn, protocol.TypeStdout, "session-recovered", protocol.StreamData{Data: []byte("retained")})
		writeProtocol(t, conn, protocol.TypeExit, "session-recovered", protocol.ExitResult{ExitCode: 9})
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	conn, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Ensure the read loop has an opportunity to receive retained delivery before
	// the durable orchestration layer calls Attach.
	time.Sleep(25 * time.Millisecond)
	session, err := conn.Attach("session-recovered")
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(session.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(stdout) != "retained" || result.ExitCode != 9 {
		t.Fatalf("stdout=%q result=%+v", stdout, result)
	}
}
