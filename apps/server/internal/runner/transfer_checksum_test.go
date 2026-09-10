package runner

import (
	"context"
	"encoding/base64"
	"testing"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func TestHandleTransferMessageRejectsCorruptPayload(t *testing.T) {
	connection := &Connection{
		transfers:       map[string]*incomingTransferState{},
		transferWaiters: map[string]*transferWaiter{},
		transferDone:    map[string]transferResult{},
		done:            make(chan struct{}),
	}
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
	if err := connection.handleTransferMessage(begin); err != nil {
		t.Fatal(err)
	}
	if err := connection.handleTransferMessage(chunk); err != nil {
		t.Fatal(err)
	}
	if err := connection.handleTransferMessage(end); err == nil {
		t.Fatal("checksum mismatch accepted")
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
