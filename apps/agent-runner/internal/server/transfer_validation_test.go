package server

import (
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestTransferHandlersRejectInvalidFrames(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	cases := []struct {
		name string
		typ  protocol.MessageType
		code string
	}{
		{name: "begin", typ: protocol.TypeTransferBegin, code: "invalid_transfer"},
		{name: "chunk", typ: protocol.TypeTransferChunk, code: "invalid_transfer"},
		{name: "end", typ: protocol.TypeTransferEnd, code: "invalid_transfer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := conn.WriteJSON(protocol.Message{
				Version: protocol.Version2, Type: tc.typ, SessionID: "validation-session", Payload: json.RawMessage(`[]`),
			}); err != nil {
				t.Fatal(err)
			}
			msg := read(t, conn)
			if msg.Type != protocol.TypeError {
				t.Fatalf("response=%+v", msg)
			}
			payload, err := protocol.DecodePayload[protocol.ErrorPayload](msg)
			if err != nil || payload.Code != tc.code {
				t.Fatalf("error=%+v decode=%v", payload, err)
			}
		})
	}
}

func TestTransferHandlersRejectInvalidState(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeTransferBegin, "state-session", protocol.TransferBegin{
		TransferID: "", Direction: protocol.TransferDirectionToRunner,
	})
	msg := read(t, conn)
	payload, err := protocol.DecodePayload[protocol.ErrorPayload](msg)
	if msg.Type != protocol.TypeError || err != nil || payload.Code != "transfer_failed" {
		t.Fatalf("invalid begin response=%+v payload=%+v err=%v", msg, payload, err)
	}

	send(t, conn, protocol.TypeTransferChunk, "state-session", protocol.TransferChunk{TransferID: "missing"})
	msg = read(t, conn)
	payload, err = protocol.DecodePayload[protocol.ErrorPayload](msg)
	if msg.Type != protocol.TypeError || err != nil || payload.Code != "transfer_failed" {
		t.Fatalf("orphan chunk response=%+v payload=%+v err=%v", msg, payload, err)
	}
}
