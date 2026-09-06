package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func requiredRunnerCapabilities() protocol.Capabilities {
	return protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}
}

func TestProtocolErrorFormattingAndConnectionMetadata(t *testing.T) {
	if got := (&ProtocolError{Code: "busy"}).Error(); got != "runner protocol error: busy" {
		t.Fatalf("Error()=%q", got)
	}
	if got := (&ProtocolError{Code: "busy", Message: "try later"}).Error(); !strings.Contains(got, "try later") {
		t.Fatalf("Error()=%q", got)
	}

	conn := &Connection{
		caps: protocol.Capabilities{MaxActiveSessions: 2, Features: []string{"stdin", "health"}},
		health: protocol.Health{Status: "ok", ActiveSessionIDs: []string{"session-1"}},
		done: make(chan struct{}),
	}
	caps := conn.Capabilities()
	health := conn.Health()
	caps.Features[0] = "changed"
	health.ActiveSessionIDs[0] = "changed"
	if conn.caps.Features[0] != "stdin" || conn.health.ActiveSessionIDs[0] != "session-1" {
		t.Fatal("metadata accessors returned aliased slices")
	}
	if conn.Done() != conn.done || conn.Err() != nil {
		t.Fatalf("Done/Err unexpected: done=%v err=%v", conn.Done() == conn.done, conn.Err())
	}
	if !errors.Is(conn.connectionError(), ErrDisconnected) {
		t.Fatalf("connectionError()=%v", conn.connectionError())
	}
	conn.err = ErrClosed
	if !errors.Is(conn.connectionError(), ErrClosed) {
		t.Fatalf("connectionError()=%v", conn.connectionError())
	}
}

func TestClientRegistrationAndPendingBufferBranches(t *testing.T) {
	if _, err := dialWith(context.Background(), nil, "ws://unused", nil); err == nil {
		t.Fatal("dialWith() accepted nil dialer")
	}

	conn := &Connection{sessions: make(map[string]*Session), pending: make(map[string]*pendingSessionMessages), done: make(chan struct{})}
	if _, err := conn.register(""); err == nil {
		t.Fatal("register() accepted blank session id")
	}
	session, err := conn.register("session-1")
	if err != nil || session.ID() != "session-1" {
		t.Fatalf("register() session=%v err=%v", session, err)
	}
	if _, err := conn.register("session-1"); !errors.Is(err, ErrSessionExists) {
		t.Fatalf("duplicate register error=%v", err)
	}
	conn.err = ErrClosed
	if _, err := conn.register("session-2"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed register error=%v", err)
	}
	conn.err = nil
	session.fail(io.EOF)
	conn.unregister("session-1", session)

	valid, err := protocol.NewMessage(protocol.Version1, protocol.TypeStdout, "buffered", protocol.StreamData{Data: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.bufferPendingLocked(valid); err != nil || len(conn.pending["buffered"].messages) != 1 {
		t.Fatalf("bufferPendingLocked() pending=%+v err=%v", conn.pending["buffered"], err)
	}
	missing := valid
	missing.SessionID = ""
	if err := conn.bufferPendingLocked(missing); err == nil {
		t.Fatal("bufferPendingLocked() accepted missing session id")
	}
	if err := conn.bufferPendingLocked(protocol.Message{Version: protocol.Version1, Type: protocol.TypeHealth}); err == nil {
		t.Fatal("bufferPendingLocked() accepted unexpected message type")
	}
	conn.pending["full"] = &pendingSessionMessages{messages: make([]protocol.Message, maxPendingSessionMessages)}
	full := valid
	full.SessionID = "full"
	if err := conn.bufferPendingLocked(full); err == nil {
		t.Fatal("bufferPendingLocked() accepted message-count overflow")
	}
	conn.pending["bytes"] = &pendingSessionMessages{bytes: maxPendingSessionBytes}
	oversize := valid
	oversize.SessionID = "bytes"
	if err := conn.bufferPendingLocked(oversize); err == nil {
		t.Fatal("bufferPendingLocked() accepted byte overflow")
	}
}

func TestDeliverSessionMessageErrorBranches(t *testing.T) {
	conn := &Connection{done: make(chan struct{})}

	started := newSession("started", conn)
	msg, _ := protocol.NewMessage(protocol.Version1, protocol.TypeSessionStarted, "started", nil)
	terminal, err := deliverSessionMessage(started, msg)
	if err != nil || terminal {
		t.Fatalf("session_started terminal=%v err=%v", terminal, err)
	}
	started.fail(io.EOF)

	protocolFailure := newSession("error", conn)
	errMsg, _ := protocol.NewMessage(protocol.Version1, protocol.TypeError, "error", protocol.ErrorPayload{Code: "rejected", Message: "no"})
	terminal, err = deliverSessionMessage(protocolFailure, errMsg)
	if err != nil || !terminal {
		t.Fatalf("error terminal=%v err=%v", terminal, err)
	}
	if _, err := protocolFailure.Wait(context.Background()); err == nil {
		t.Fatal("session error did not reach waiter")
	}

	malformedStream := newSession("stream", conn)
	terminal, err = deliverSessionMessage(malformedStream, protocol.Message{Version: protocol.Version1, Type: protocol.TypeStdout, SessionID: "stream", Payload: json.RawMessage(`{"data":`)})
	if err == nil || terminal {
		t.Fatalf("malformed stdout terminal=%v err=%v", terminal, err)
	}
	malformedStream.fail(io.EOF)

	malformedExit := newSession("exit", conn)
	terminal, err = deliverSessionMessage(malformedExit, protocol.Message{Version: protocol.Version1, Type: protocol.TypeExit, SessionID: "exit", Payload: json.RawMessage(`{"exit_code":"bad"}`)})
	if err == nil || !terminal {
		t.Fatalf("malformed exit terminal=%v err=%v", terminal, err)
	}
	malformedExit.fail(io.EOF)

	unexpected := newSession("unexpected", conn)
	terminal, err = deliverSessionMessage(unexpected, protocol.Message{Version: protocol.Version1, Type: protocol.TypeStdin, SessionID: "unexpected"})
	if err == nil || terminal {
		t.Fatalf("unexpected terminal=%v err=%v", terminal, err)
	}
	unexpected.fail(io.EOF)

	validError, _ := protocol.NewMessage(protocol.Version1, protocol.TypeError, "", protocol.ErrorPayload{Code: "bad", Message: "payload"})
	if err := protocolErrorFromMessage(validError); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("protocolErrorFromMessage()=%v", err)
	}
	invalidError := validError
	invalidError.Payload = json.RawMessage(`{"code":`)
	if err := protocolErrorFromMessage(invalidError); err == nil {
		t.Fatal("protocolErrorFromMessage() accepted malformed payload")
	}
}

func TestClosedConnectionSignalAndStdinBranches(t *testing.T) {
	closed := &Connection{done: make(chan struct{}), err: ErrClosed}
	close(closed.done)
	if err := closed.writeSessionSignal("session", false); !errors.Is(err, ErrClosed) {
		t.Fatalf("writeSessionSignal() error=%v", err)
	}

	received := make(chan protocol.Message, 8)
	server := newProtocolTestServer(t, requiredRunnerCapabilities(), func(conn *websocket.Conn, msg protocol.Message) {
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
	session, err := conn.Start(context.Background(), "session-kill", Request{Command: []string{"sleep", "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Kill(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Stdin().Close(); err != nil {
		t.Fatalf("second stdin close=%v", err)
	}
	if _, err := session.Stdin().Write([]byte("late")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write after close error=%v", err)
	}
	seen := map[protocol.MessageType]bool{}
	for len(seen) < 3 {
		msg := <-received
		seen[msg.Type] = true
	}
	for _, typ := range []protocol.MessageType{protocol.TypeStart, protocol.TypeKill, protocol.TypeStdinClose} {
		if !seen[typ] {
			t.Fatalf("missing %s in %v", typ, seen)
		}
	}
}

func TestClientHandshakeRejectionBranches(t *testing.T) {
	caps := requiredRunnerCapabilities()
	goodHello, _ := protocol.NewMessage(protocol.Version1, protocol.TypeRunnerHello, "", protocol.RunnerHello{Version: protocol.Version1, Capabilities: caps})
	goodHealth, _ := protocol.NewMessage(protocol.Version1, protocol.TypeHealth, "", protocol.Health{Status: "ok"})
	tests := []struct {
		name     string
		messages []protocol.Message
	}{
		{name: "runner error", messages: []protocol.Message{mustProtocolMessage(t, protocol.TypeError, "", protocol.ErrorPayload{Code: "nope"})}},
		{name: "wrong hello type", messages: []protocol.Message{goodHealth}},
		{name: "invalid hello payload", messages: []protocol.Message{{Version: protocol.Version1, Type: protocol.TypeRunnerHello, Payload: json.RawMessage(`{"version":"bad"}`)}}},
		{name: "unsupported hello version", messages: []protocol.Message{mustProtocolMessage(t, protocol.TypeRunnerHello, "", protocol.RunnerHello{Version: 2, Capabilities: caps})}},
		{name: "zero capacity", messages: []protocol.Message{mustProtocolMessage(t, protocol.TypeRunnerHello, "", protocol.RunnerHello{Version: protocol.Version1, Capabilities: protocol.Capabilities{Features: caps.Features}})}},
		{name: "wrong health type", messages: []protocol.Message{goodHello, goodHello}},
		{name: "invalid health payload", messages: []protocol.Message{goodHello, protocol.Message{Version: protocol.Version1, Type: protocol.TypeHealth, Payload: json.RawMessage(`{"active_sessions":"bad"}`)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newHandshakeSequenceServer(t, tt.messages)
			defer server.Close()
			if _, err := Dial(context.Background(), wsURL(server.URL)); err == nil {
				t.Fatal("Dial() unexpectedly succeeded")
			}
		})
	}
}

func mustProtocolMessage(t *testing.T, typ protocol.MessageType, sessionID string, payload any) protocol.Message {
	t.Helper()
	msg, err := protocol.NewMessage(protocol.Version1, typ, sessionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func newHandshakeSequenceServer(t *testing.T, messages []protocol.Message) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		for _, msg := range messages {
			data, err := protocol.Encode(msg)
			if err != nil {
				t.Errorf("encode protocol message: %v", err)
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}))
}
