package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func TestClientReturnsUncertainSessionWhenStartContextExpires(t *testing.T) {
	release := make(chan struct{})
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}, func(conn *websocket.Conn, msg protocol.Message) {
		if msg.Type != protocol.TypeStart {
			return
		}
		<-release
		if err := writeProtocol(conn, protocol.TypeSessionStarted, msg.SessionID, nil); err != nil {
			t.Errorf("write session_started: %v", err)
			return
		}
		if err := writeProtocol(conn, protocol.TypeExit, msg.SessionID, protocol.ExitResult{ExitCode: 0}); err != nil {
			t.Errorf("write exit: %v", err)
			return
		}
	})
	defer server.Close()

	conn, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	session, err := conn.Start(ctx, "session-uncertain", Request{Command: []string{"true"}})
	if !errors.Is(err, context.DeadlineExceeded) || session == nil {
		t.Fatalf("session=%v err=%v", session, err)
	}
	close(release)
	result, err := session.Wait(context.Background())
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
