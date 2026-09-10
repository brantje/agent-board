package runner

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

var transferTestFeatures = []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}

func TestConnectionSendsTransferAndAppliedAcknowledgement(t *testing.T) {
	received := make(chan protocol.Message, 4)
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: transferTestFeatures}, func(_ *websocket.Conn, msg protocol.Message) {
		received <- msg
	})
	defer server.Close()

	connection, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	payload := []byte("workspace payload")
	var progress TransferProgress
	if err := connection.SendTransfer(context.Background(), "session-1", "transfer-1", "to_runner", payload, func(value TransferProgress) {
		progress = value
	}); err != nil {
		t.Fatal(err)
	}
	if err := connection.ConfirmTransferApplied(context.Background(), "session-1", "transfer-1"); err != nil {
		t.Fatal(err)
	}
	if progress.BytesTransferred != int64(len(payload)) || progress.TotalBytes != int64(len(payload)) {
		t.Fatalf("progress=%+v", progress)
	}

	messages := make([]protocol.Message, 0, 4)
	for len(messages) < 4 {
		select {
		case msg := <-received:
			messages = append(messages, msg)
		case <-time.After(time.Second):
			t.Fatalf("received %d transfer messages", len(messages))
		}
	}
	want := []protocol.MessageType{protocol.TypeTransferBegin, protocol.TypeTransferChunk, protocol.TypeTransferEnd, protocol.TypeTransferApplied}
	for i, typ := range want {
		if messages[i].Type != typ || messages[i].SessionID != "session-1" {
			t.Fatalf("message[%d]=%+v", i, messages[i])
		}
	}
	begin, err := protocol.DecodePayload[protocol.TransferBegin](messages[0])
	if err != nil || begin.TransferID != "transfer-1" || begin.Direction != "to_runner" || begin.Checksum != protocol.TransferChecksum(payload) {
		t.Fatalf("begin=%+v err=%v", begin, err)
	}
}

func TestConnectionReceivesRequestedWorkspaceTransfer(t *testing.T) {
	payload := []byte("runner workspace changes")
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: transferTestFeatures}, func(conn *websocket.Conn, msg protocol.Message) {
		if msg.Type != protocol.TypeTransferEnd || msg.SessionID != "session-2" {
			return
		}
		if err := writeProtocol(conn, protocol.TypeTransferBegin, msg.SessionID, protocol.TransferBegin{
			TransferID: "returned-1", Direction: "from_runner", TotalBytes: int64(len(payload)), Checksum: protocol.TransferChecksum(payload),
		}); err != nil {
			t.Errorf("write transfer begin: %v", err)
			return
		}
		if err := writeProtocol(conn, protocol.TypeTransferChunk, msg.SessionID, protocol.TransferChunk{
			TransferID: "returned-1", Data: base64.StdEncoding.EncodeToString(payload),
		}); err != nil {
			t.Errorf("write transfer chunk: %v", err)
			return
		}
		if err := writeProtocol(conn, protocol.TypeTransferEnd, msg.SessionID, protocol.TransferEnd{TransferID: "returned-1"}); err != nil {
			t.Errorf("write transfer end: %v", err)
		}
	})
	defer server.Close()

	connection, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	type receiveResult struct {
		id       string
		payload  []byte
		progress TransferProgress
		err      error
	}
	done := make(chan receiveResult, 1)
	go func() {
		var progress TransferProgress
		id, data, err := connection.ReceiveTransfer(context.Background(), "session-2", func(value TransferProgress) {
			progress = value
		})
		done <- receiveResult{id: id, payload: data, progress: progress, err: err}
	}()

	if err := connection.SendTransfer(context.Background(), "session-2", "request-1", "from_runner", nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.id != "returned-1" || string(result.payload) != string(payload) {
			t.Fatalf("result=%+v", result)
		}
		if result.progress.BytesTransferred != int64(len(payload)) || result.progress.TotalBytes != int64(len(payload)) {
			t.Fatalf("progress=%+v", result.progress)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for returned workspace transfer")
	}
}
