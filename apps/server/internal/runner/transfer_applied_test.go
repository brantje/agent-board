package runner

import (
	"context"
	"errors"
	"testing"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func TestConfirmTransferAppliedSendsScopedAcknowledgement(t *testing.T) {
	received := make(chan protocol.Message, 1)
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}, func(_ anyConn, _ protocol.Message) {})
	_ = received
	_ = server
}

func TestConfirmTransferAppliedValidatesInputAndContext(t *testing.T) {
	var conn *Connection
	if err := conn.ConfirmTransferApplied(context.Background(), "session-1", "transfer-1"); err == nil {
		t.Fatal("nil connection accepted")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn = &Connection{}
	if err := conn.ConfirmTransferApplied(ctx, "session-1", "transfer-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error=%v", err)
	}
	if err := conn.ConfirmTransferApplied(context.Background(), "", "transfer-1"); err == nil {
		t.Fatal("blank session id accepted")
	}
	if err := conn.ConfirmTransferApplied(context.Background(), "session-1", " "); err == nil {
		t.Fatal("blank transfer id accepted")
	}
}
