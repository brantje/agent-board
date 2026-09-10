package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	"github.com/gorilla/websocket"
)

type callbackWriteConn struct {
	onMessage func([]byte)
}

func (c *callbackWriteConn) SetWriteDeadline(time.Time) error { return nil }

func (c *callbackWriteConn) WriteMessage(messageType int, data []byte) error {
	if messageType == websocket.TextMessage && c.onMessage != nil {
		c.onMessage(data)
	}
	return nil
}

func (c *callbackWriteConn) WriteControl(int, []byte, time.Time) error { return nil }

type failingWriteConn struct {
	attempted chan struct{}
}

func (c *failingWriteConn) SetWriteDeadline(time.Time) error { return nil }
func (c *failingWriteConn) WriteControl(int, []byte, time.Time) error { return nil }
func (c *failingWriteConn) WriteMessage(int, []byte) error {
	select {
	case <-c.attempted:
	default:
		close(c.attempted)
	}
	return errors.New("connection lost")
}

func TestHandleStartRegistersDeliveryBeforeSessionStarted(t *testing.T) {
	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	checked := false
	conn := &callbackWriteConn{}
	writer := &connectionWriter{conn: conn}
	conn.onMessage = func(data []byte) {
		msg, err := protocol.Decode(data)
		if err != nil || msg.Type != protocol.TypeSessionStarted {
			return
		}
		checked = true
		runner.deliveryMu.Lock()
		_, registered := runner.deliveries["ordering"]
		runner.deliveryMu.Unlock()
		if !registered {
			t.Error("session delivery was not registered before session_started")
		}
	}

	msg, err := protocol.NewMessage(protocol.Version2, protocol.TypeStart, "ordering", protocol.StartRequest{
		Command: []string{"true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner.handleStart(writer, msg)
	if !checked {
		t.Fatal("session_started was not emitted")
	}

	waitFor(t, time.Second, func() bool { return runner.manager.ActiveCount() == 0 })
}

func TestSessionDeliveryResumesWorkspaceTransferAfterReconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	attempted := make(chan struct{})
	delivery := newSessionDelivery(ctx, &connectionWriter{conn: &failingWriteConn{attempted: attempted}}, time.Second)
	done := make(chan error, 1)
	go func() {
		done <- delivery.sendTransfer(ctx, "session-1", "transfer-1", "from_runner", []byte("workspace"))
	}()

	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("initial transfer write was not attempted")
	}

	messages := make(chan protocol.Message, 4)
	delivery.attach(&connectionWriter{conn: &callbackWriteConn{onMessage: func(data []byte) {
		msg, err := protocol.Decode(data)
		if err != nil {
			t.Errorf("decode resumed transfer: %v", err)
			return
		}
		messages <- msg
	}}})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resumed transfer failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("resumed transfer did not complete")
	}

	var types []protocol.MessageType
	for len(types) < 3 {
		select {
		case msg := <-messages:
			types = append(types, msg.Type)
		case <-time.After(time.Second):
			t.Fatalf("resumed transfer emitted %v", types)
		}
	}
	want := []protocol.MessageType{protocol.TypeTransferBegin, protocol.TypeTransferChunk, protocol.TypeTransferEnd}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("resumed transfer message types=%v", types)
		}
	}
}
