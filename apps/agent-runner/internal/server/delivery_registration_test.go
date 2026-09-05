package server

import (
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

	msg, err := protocol.NewMessage(protocol.Version1, protocol.TypeStart, "ordering", protocol.StartRequest{
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
