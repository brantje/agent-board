package runner

import (
	"encoding/json"
	"errors"
	"testing"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func newDispatchTestConnection() *Connection {
	return &Connection{
		sessions:        make(map[string]*Session),
		pending:         make(map[string]*pendingSessionMessages),
		connects:        make(map[string]map[string]*sessionConn),
		transfers:       make(map[string]*incomingTransferState),
		transferWaiters: make(map[string]*transferWaiter),
		transferDone:    make(map[string]transferResult),
		done:            make(chan struct{}),
	}
}

func TestConnectionDispatchesControlAndUnattachedSessionMessages(t *testing.T) {
	connection := newDispatchTestConnection()

	health := transferMessage(t, protocol.TypeHealth, "", protocol.Health{Status: "ok", ActiveSessions: 2})
	if err := connection.handleMessage(health); err != nil {
		t.Fatal(err)
	}
	if got := connection.Health(); got.Status != "ok" || got.ActiveSessions != 2 {
		t.Fatalf("health=%+v", got)
	}

	malformedHealth := health
	malformedHealth.Payload = json.RawMessage(`{"status":`)
	if err := connection.handleMessage(malformedHealth); err == nil {
		t.Fatal("malformed health payload was accepted")
	}

	globalError := transferMessage(t, protocol.TypeError, "", protocol.ErrorPayload{Code: "runner_busy", Message: "busy"})
	err := connection.handleMessage(globalError)
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Code != "runner_busy" {
		t.Fatalf("global error=%v", err)
	}

	stdout := transferMessage(t, protocol.TypeStdout, "session-pending", protocol.StreamData{Data: []byte("buffer me")})
	if err := connection.handleMessage(stdout); err != nil {
		t.Fatal(err)
	}
	pending := connection.pending["session-pending"]
	if pending == nil || len(pending.messages) != 1 {
		t.Fatalf("pending=%+v", pending)
	}
}

func TestConnectionDispatchCleansMalformedAndTerminalSessions(t *testing.T) {
	connection := newDispatchTestConnection()

	malformed := newSession("session-malformed", connection)
	connection.sessions[malformed.ID()] = malformed
	badStdout := transferMessage(t, protocol.TypeStdout, malformed.ID(), protocol.StreamData{Data: []byte("ignored")})
	badStdout.Payload = json.RawMessage(`{"data":`)
	if err := connection.handleMessage(badStdout); err != nil {
		t.Fatalf("dispatcher leaked session decode failure: %v", err)
	}
	if _, ok := connection.sessions[malformed.ID()]; ok {
		t.Fatal("malformed session was not unregistered")
	}
	if _, err := malformed.Wait(t.Context()); err == nil {
		t.Fatal("malformed session did not retain failure")
	}

	terminal := newSession("session-terminal", connection)
	connection.sessions[terminal.ID()] = terminal
	exit := transferMessage(t, protocol.TypeExit, terminal.ID(), protocol.ExitResult{ExitCode: 17})
	if err := connection.handleMessage(exit); err != nil {
		t.Fatal(err)
	}
	if _, ok := connection.sessions[terminal.ID()]; ok {
		t.Fatal("terminal session was not unregistered")
	}
	result, err := terminal.Wait(t.Context())
	if err != nil || result.ExitCode != 17 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
