package server

import (
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	"github.com/gorilla/websocket"
)

type recordingWebSocketWriteConn struct {
	calls       []string
	deadline    time.Time
	deadlineErr error
	messageType int
}

func (c *recordingWebSocketWriteConn) SetWriteDeadline(deadline time.Time) error {
	c.calls = append(c.calls, "deadline")
	c.deadline = deadline
	return c.deadlineErr
}

func (c *recordingWebSocketWriteConn) WriteMessage(messageType int, _ []byte) error {
	c.calls = append(c.calls, "message")
	c.messageType = messageType
	return nil
}

func (c *recordingWebSocketWriteConn) WriteControl(int, []byte, time.Time) error {
	c.calls = append(c.calls, "control")
	return nil
}

func TestConnectionWriterSetsDeadlineBeforeDataWrite(t *testing.T) {
	conn := &recordingWebSocketWriteConn{}
	writer := &connectionWriter{conn: conn}
	before := time.Now()

	if err := writer.send(protocol.TypeHealth, "", protocol.Health{Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	if len(conn.calls) != 2 || conn.calls[0] != "deadline" || conn.calls[1] != "message" {
		t.Fatalf("unexpected write call order %#v", conn.calls)
	}
	if conn.messageType != websocket.TextMessage {
		t.Fatalf("unexpected message type %d", conn.messageType)
	}
	if !conn.deadline.After(before) || conn.deadline.After(time.Now().Add(writeTimeout+time.Second)) {
		t.Fatalf("unexpected write deadline %v", conn.deadline)
	}
}

func TestConnectionWriterDoesNotWriteWhenDeadlineFails(t *testing.T) {
	deadlineErr := errors.New("deadline failed")
	conn := &recordingWebSocketWriteConn{deadlineErr: deadlineErr}
	writer := &connectionWriter{conn: conn}

	if err := writer.send(protocol.TypeHealth, "", protocol.Health{Status: "ok"}); !errors.Is(err, deadlineErr) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	if len(conn.calls) != 1 || conn.calls[0] != "deadline" {
		t.Fatalf("write should stop after deadline failure, calls=%#v", conn.calls)
	}
}
