package runner

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func TestClientStartCancellationRetainsSentSessionForTerminalDelivery(t *testing.T) {
	startSeen := make(chan struct{})
	release := make(chan struct{})
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}, func(conn *websocket.Conn, msg protocol.Message) {
		if msg.Type != protocol.TypeStart {
			return
		}
		close(startSeen)
		<-release
		if err := writeProtocol(conn, protocol.TypeSessionStarted, msg.SessionID, nil); err != nil {
			t.Errorf("write session_started: %v", err)
			return
		}
		if err := writeProtocol(conn, protocol.TypeExit, msg.SessionID, protocol.ExitResult{ExitCode: 0}); err != nil {
			t.Errorf("write exit: %v", err)
		}
	})
	defer server.Close()

	conn, err := Dial(t.Context(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan struct {
		session ProcessSession
		err     error
	}, 1)
	go func() {
		session, startErr := conn.Start(ctx, "session-cancel", Request{Command: []string{"true"}})
		result <- struct {
			session ProcessSession
			err     error
		}{session: session, err: startErr}
	}()

	select {
	case <-startSeen:
	case <-time.After(time.Second):
		t.Fatal("runner did not receive start request")
	}
	cancel()
	started := <-result
	if !errors.Is(started.err, context.Canceled) || started.session == nil {
		t.Fatalf("Start session=%v err=%v, want retained session plus context.Canceled", started.session, started.err)
	}
	close(release)
	terminal, err := started.session.Wait(t.Context())
	if err != nil || terminal.ExitCode != 0 {
		t.Fatalf("retained terminal result=%+v err=%v", terminal, err)
	}
}

func TestClientStartProtocolRejectionReleasesSessionRegistration(t *testing.T) {
	var starts atomic.Int32
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}, func(conn *websocket.Conn, msg protocol.Message) {
		if msg.Type != protocol.TypeStart {
			return
		}
		if starts.Add(1) == 1 {
			if err := writeProtocol(conn, protocol.TypeError, msg.SessionID, protocol.ErrorPayload{Code: "start_rejected", Message: "not accepted"}); err != nil {
				t.Errorf("write rejection: %v", err)
			}
			return
		}
		if err := writeProtocol(conn, protocol.TypeSessionStarted, msg.SessionID, nil); err != nil {
			t.Errorf("write session_started: %v", err)
			return
		}
		if err := writeProtocol(conn, protocol.TypeExit, msg.SessionID, protocol.ExitResult{ExitCode: 0}); err != nil {
			t.Errorf("write exit: %v", err)
		}
	})
	defer server.Close()

	conn, err := Dial(t.Context(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if session, err := conn.Start(t.Context(), "session-retry", Request{Command: []string{"true"}}); session != nil {
		t.Fatalf("rejected start retained session=%v", session)
	} else {
		var protocolErr *ProtocolError
		if !errors.As(err, &protocolErr) || protocolErr.Code != "start_rejected" {
			t.Fatalf("rejected start error=%v", err)
		}
	}

	session, err := conn.Start(t.Context(), "session-retry", Request{Command: []string{"true"}})
	if err != nil {
		t.Fatalf("retry after protocol rejection: %v", err)
	}
	if _, err := session.Wait(t.Context()); err != nil {
		t.Fatalf("retry terminal result: %v", err)
	}
}

func TestClientStartDisconnectBeforeConfirmationIsNotOwnedLocally(t *testing.T) {
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}, func(conn *websocket.Conn, msg protocol.Message) {
		if msg.Type == protocol.TypeStart {
			_ = conn.Close()
		}
	})
	defer server.Close()

	conn, err := Dial(t.Context(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	session, err := conn.Start(t.Context(), "session-disconnect", Request{Command: []string{"true"}})
	if session != nil || !errors.Is(err, ErrDisconnected) {
		t.Fatalf("Start session=%v err=%v, want nil session and ErrDisconnected", session, err)
	}
}
