package server

import (
	"bytes"
	"testing"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestBlockedProcessStdinDoesNotPreventKill(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "blocked-stdin", protocol.StartRequest{
		Command: []string{"sh", "-c", "sleep 30"},
	})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", msg)
	}

	chunk := bytes.Repeat([]byte("x"), 256*1024)
	for i := 0; i < stdinQueueCapacity+8; i++ {
		send(t, conn, protocol.TypeStdin, "blocked-stdin", protocol.StreamData{Data: chunk})
	}
	send(t, conn, protocol.TypeKill, "blocked-stdin", nil)

	sawBackpressure := false
	for {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeError:
			payload, err := protocol.DecodePayload[protocol.ErrorPayload](msg)
			if err != nil {
				t.Fatal(err)
			}
			if payload.Code == "stdin_backpressure" {
				sawBackpressure = true
			}
		case protocol.TypeExit:
			result, err := protocol.DecodePayload[protocol.ExitResult](msg)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Signaled {
				t.Fatalf("expected killed process, got %#v", result)
			}
			if !sawBackpressure {
				t.Fatal("expected bounded stdin queue to report backpressure")
			}
			return
		}
	}
}
