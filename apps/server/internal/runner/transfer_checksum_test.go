package runner

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

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

func TestTransferRequiresSessionIdentity(t *testing.T) {
	var conn *Connection
	if err := conn.SendTransfer(context.Background(), "", "", "to_runner", nil, nil); err == nil {
		t.Fatal("blank transfer accepted")
	}
	if _, _, err := conn.ReceiveTransfer(context.Background(), "", nil); err == nil {
		t.Fatal("blank receive accepted")
	}
}

func TestReceiveTransferReturnsCompletedResultAndCancellation(t *testing.T) {
	c := &Connection{
		transfers:       map[string]*incomingTransferState{},
		transferWaiters: map[string]*transferWaiter{},
		transferDone:    map[string]transferResult{"session-1": {transferID: "t1", payload: []byte("ok")}},
		done:            make(chan struct{}),
	}
	id, payload, err := c.ReceiveTransfer(context.Background(), "session-1", nil)
	if err != nil || id != "t1" || string(payload) != "ok" {
		t.Fatalf("completed transfer id=%q payload=%q err=%v", id, payload, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.ReceiveTransfer(ctx, "session-2", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled receive, got %v", err)
	}

	failed, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferFailed, "session-3", protocol.TransferFailed{TransferID: "t3", Message: "runner disconnected"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(failed); err == nil {
		t.Fatal("transfer failed message accepted")
	}
	if err := c.handleTransferMessage(protocol.Message{Type: protocol.TypeTransferBegin}); err == nil {
		t.Fatal("missing session accepted")
	}
}

func TestHandleTransferMessageRejectsInvalidProtocolAndClosedReceive(t *testing.T) {
	c := &Connection{
		transfers:       map[string]*incomingTransferState{},
		transferWaiters: map[string]*transferWaiter{},
		transferDone:    map[string]transferResult{},
		done:            make(chan struct{}),
	}
	if err := c.handleTransferMessage(protocol.Message{Type: protocol.TypeTransferBegin, SessionID: "session-1", Payload: []byte(`{`)}); err == nil {
		t.Fatal("invalid begin payload accepted")
	}
	invalidBegin, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferBegin, "session-1", protocol.TransferBegin{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(invalidBegin); err == nil {
		t.Fatal("invalid begin accepted")
	}
	if err := c.handleTransferMessage(protocol.Message{Type: protocol.TypeStart, SessionID: "session-1"}); err == nil {
		t.Fatal("unexpected transfer type accepted")
	}

	zeroBegin, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferBegin, "session-zero", protocol.TransferBegin{
		TransferID: "t0", Direction: "from_runner", TotalBytes: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(zeroBegin); err != nil {
		t.Fatal(err)
	}
	zeroChunk, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferChunk, "session-zero", protocol.TransferChunk{
		TransferID: "t0", Data: base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(zeroChunk); err == nil {
		t.Fatal("undeclared payload accepted")
	}

	if err := c.handleTransferMessage(protocol.Message{Type: protocol.TypeTransferChunk, SessionID: "missing", Payload: []byte(`{"transfer_id":"t","data":"QQ=="}`)}); err != nil {
		t.Fatalf("chunk without begin: %v", err)
	}
	if err := c.handleTransferMessage(protocol.Message{Type: protocol.TypeTransferEnd, SessionID: "missing", Payload: []byte(`{"transfer_id":"t"}`)}); err != nil {
		t.Fatalf("end without begin: %v", err)
	}
	if err := c.handleTransferMessage(protocol.Message{Type: protocol.TypeTransferChunk, SessionID: "session-zero", Payload: []byte(`{"transfer_id":"t0","data":"%%%"}`)}); err == nil {
		t.Fatal("invalid chunk encoding accepted")
	}

	mismatchBegin, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferBegin, "session-mismatch", protocol.TransferBegin{
		TransferID: "keep", Direction: "from_runner", TotalBytes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(mismatchBegin); err != nil {
		t.Fatal(err)
	}
	wrongChunk, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferChunk, "session-mismatch", protocol.TransferChunk{
		TransferID: "other", Data: base64.StdEncoding.EncodeToString([]byte("abcd")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(wrongChunk); err != nil {
		t.Fatalf("mismatched chunk id: %v", err)
	}
	wrongEnd, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferEnd, "session-mismatch", protocol.TransferEnd{TransferID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(wrongEnd); err != nil {
		t.Fatalf("mismatched end id: %v", err)
	}

	sizeBegin, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferBegin, "session-size", protocol.TransferBegin{
		TransferID: "ts", Direction: "from_runner", TotalBytes: 4, Checksum: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(sizeBegin); err != nil {
		t.Fatal(err)
	}
	shortChunk, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferChunk, "session-size", protocol.TransferChunk{
		TransferID: "ts", Data: base64.StdEncoding.EncodeToString([]byte("ab")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(shortChunk); err != nil {
		t.Fatal(err)
	}
	sizeEnd, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferEnd, "session-size", protocol.TransferEnd{TransferID: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleTransferMessage(sizeEnd); err == nil {
		t.Fatal("short transfer accepted")
	}

	closed := &Connection{
		transfers:       map[string]*incomingTransferState{},
		transferWaiters: map[string]*transferWaiter{},
		transferDone:    map[string]transferResult{},
		done:            make(chan struct{}),
	}
	close(closed.done)
	if _, _, err := closed.ReceiveTransfer(context.Background(), "session-closed", nil); err == nil {
		t.Fatal("closed receive accepted")
	}

	waiting := &Connection{
		transfers:       map[string]*incomingTransferState{},
		transferWaiters: map[string]*transferWaiter{},
		transferDone:    map[string]transferResult{},
		done:            make(chan struct{}),
	}
	failed, err := protocol.NewMessage(protocol.Version2, protocol.TypeTransferFailed, "session-wait", protocol.TransferFailed{TransferID: "tw", Message: "lost"})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, _, err := waiting.ReceiveTransfer(context.Background(), "session-wait", nil)
		result <- err
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		waiting.mu.Lock()
		_, ok := waiting.transferWaiters["session-wait"]
		waiting.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := waiting.handleTransferMessage(failed); err == nil {
		t.Fatal("failed transfer with waiter accepted")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("waiter did not observe transfer failure")
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not complete")
	}
}
