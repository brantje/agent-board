package runner

import (
	"encoding/base64"
	"testing"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func TestHandleTransferMessageRejectsChecksumAndSizeMismatch(t *testing.T) {
	c := &Connection{
		transfers:       map[string]*incomingTransferState{},
		transferWaiters: map[string]*transferWaiter{},
		transferDone:    map[string]transferResult{},
		done:            make(chan struct{}),
	}
	begin, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferBegin, "session-1", protocol.TransferBegin{
		TransferID: "t1", Direction: "from_runner", TotalBytes: 4, Checksum: "not-the-hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(begin); err != nil {
		t.Fatal(err)
	}
	chunk, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferChunk, "session-1", protocol.TransferChunk{
		TransferID: "t1", Data: base64.StdEncoding.EncodeToString([]byte("abcd")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(chunk); err != nil {
		t.Fatal(err)
	}
	end, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferEnd, "session-1", protocol.TransferEnd{TransferID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(end); err == nil {
		t.Fatal("checksum mismatch accepted")
	}

	oversizeBegin, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferBegin, "session-2", protocol.TransferBegin{
		TransferID: "t2", Direction: "from_runner", TotalBytes: 1, Checksum: "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(oversizeBegin); err != nil {
		t.Fatal(err)
	}
	oversize, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferChunk, "session-2", protocol.TransferChunk{
		TransferID: "t2", Data: base64.StdEncoding.EncodeToString([]byte("abcd")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(oversize); err == nil {
		t.Fatal("oversize chunk accepted")
	}
}
