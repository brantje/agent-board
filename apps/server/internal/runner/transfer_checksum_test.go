package runner

import (
	"context"
	"encoding/base64"
	"testing"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func TestHandleTransferMessageRejectsCorruptOrOversizedPayload(t *testing.T) {
	connection := func() *Connection {
		return &Connection{
			transfers:       map[string]*incomingTransferState{},
			transferWaiters: map[string]*transferWaiter{},
			transferDone:    map[string]transferResult{},
			done:            make(chan struct{}),
		}
	}

	corrupt := connection()
	begin, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferBegin, "session-1", protocol.TransferBegin{
		TransferID: "transfer-1", Direction: "from_runner", TotalBytes: 4, Checksum: "wrong",
	})
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferChunk, "session-1", protocol.TransferChunk{
		TransferID: "transfer-1", Data: base64.StdEncoding.EncodeToString([]byte("data")),
	})
	if err != nil {
		t.Fatal(err)
	}
	end, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferEnd, "session-1", protocol.TransferEnd{TransferID: "transfer-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := corrupt.handleTransferMessage(begin); err != nil {
		t.Fatal(err)
	}
	if err := corrupt.handleTransferMessage(chunk); err != nil {
		t.Fatal(err)
	}
	if err := corrupt.handleTransferMessage(end); err == nil {
		t.Fatal("checksum mismatch accepted")
	}

	oversized := connection()
	begin, err = protocol.NewMessage(protocol.Version2, protocol.TypeTransferBegin, "session-2", protocol.TransferBegin{
		TransferID: "transfer-2", Direction: "from_runner", TotalBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	chunk, err = protocol.NewMessage(protocol.Version2, protocol.TypeTransferChunk, "session-2", protocol.TransferChunk{
		TransferID: "transfer-2", Data: base64.StdEncoding.EncodeToString([]byte("too large")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := oversized.handleTransferMessage(begin); err != nil {
		t.Fatal(err)
	}
	if err := oversized.handleTransferMessage(chunk); err == nil {
		t.Fatal("payload larger than its declared size was accepted")
	}
}

func TestReceiveTransferSurfacesRunnerFailure(t *testing.T) {
	connection := &Connection{
		transfers:       map[string]*incomingTransferState{},
		transferWaiters: map[string]*transferWaiter{},
		transferDone:    map[string]transferResult{},
		done:            make(chan struct{}),
	}
	failed, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferFailed, "session-1", protocol.TransferFailed{
		TransferID: "transfer-1", Message: "runner disconnected",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.handleTransferMessage(failed); err == nil {
		t.Fatal("runner transfer failure was not surfaced")
	}
	if _, _, err := connection.ReceiveTransfer(context.Background(), "session-1", nil); err == nil {
		t.Fatal("completed transfer failure was not returned to the receiver")
	}
}
