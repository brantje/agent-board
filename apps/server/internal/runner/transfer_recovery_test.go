package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

func newTransferStateConnection() *Connection {
	return &Connection{
		transfers:       make(map[string]*incomingTransferState),
		transferWaiters: make(map[string]*transferWaiter),
		transferDone:    make(map[string]transferResult),
		done:            make(chan struct{}),
	}
}

func transferMessage(t *testing.T, typ protocol.MessageType, sessionID string, payload any) protocol.Message {
	t.Helper()
	msg, err := protocol.NewMessage(protocol.Version2, typ, sessionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestReceiveTransferConsumesRetainedResultAndCleansCancelledWaiter(t *testing.T) {
	connection := newTransferStateConnection()
	payload := []byte("retained runner workspace")
	if err := connection.completeTransfer("session-retained", transferResult{transferID: "transfer-retained", payload: payload}); err != nil {
		t.Fatal(err)
	}

	transferID, received, err := connection.ReceiveTransfer(context.Background(), "session-retained", nil)
	if err != nil || transferID != "transfer-retained" || string(received) != string(payload) {
		t.Fatalf("retained result id=%q payload=%q err=%v", transferID, received, err)
	}
	if _, ok := connection.transferDone["session-retained"]; ok {
		t.Fatal("retained transfer result was not consumed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := connection.ReceiveTransfer(ctx, "session-cancelled", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled receive error=%v", err)
	}
	if _, ok := connection.transferWaiters["session-cancelled"]; ok {
		t.Fatal("cancelled transfer waiter was retained")
	}
}

func TestReceiveTransferReturnsConnectionFailure(t *testing.T) {
	connection := newTransferStateConnection()
	want := errors.New("runner transport lost")
	connection.err = want
	close(connection.done)

	if _, _, err := connection.ReceiveTransfer(context.Background(), "session-1", nil); !errors.Is(err, want) {
		t.Fatalf("ReceiveTransfer() error=%v", err)
	}
}

func TestTransferFailureReleasesBufferedState(t *testing.T) {
	connection := newTransferStateConnection()
	begin := transferMessage(t, protocol.TypeTransferBegin, "session-1", protocol.TransferBegin{
		TransferID: "transfer-1", Direction: "from_runner", TotalBytes: 1,
	})
	if err := connection.handleTransferMessage(begin); err != nil {
		t.Fatal(err)
	}
	badChunk := transferMessage(t, protocol.TypeTransferChunk, "session-1", protocol.TransferChunk{
		TransferID: "transfer-1", Data: "not-base64!",
	})
	if err := connection.handleTransferMessage(badChunk); err == nil {
		t.Fatal("invalid transfer chunk was accepted")
	}
	if _, ok := connection.transfers["session-1"]; ok {
		t.Fatal("invalid transfer left buffered state behind")
	}
	if _, _, err := connection.ReceiveTransfer(context.Background(), "session-1", nil); err == nil {
		t.Fatal("invalid transfer failure was not retained for receiver")
	}

	begin = transferMessage(t, protocol.TypeTransferBegin, "session-2", protocol.TransferBegin{
		TransferID: "transfer-2", Direction: "from_runner", TotalBytes: 3, Checksum: protocol.TransferChecksum([]byte("xyz")),
	})
	if err := connection.handleTransferMessage(begin); err != nil {
		t.Fatal(err)
	}
	chunk := transferMessage(t, protocol.TypeTransferChunk, "session-2", protocol.TransferChunk{
		TransferID: "transfer-2", Data: base64.StdEncoding.EncodeToString([]byte("abc")),
	})
	if err := connection.handleTransferMessage(chunk); err != nil {
		t.Fatal(err)
	}
	end := transferMessage(t, protocol.TypeTransferEnd, "session-2", protocol.TransferEnd{TransferID: "transfer-2"})
	if err := connection.handleTransferMessage(end); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum mismatch error=%v", err)
	}
	if _, ok := connection.transfers["session-2"]; ok {
		t.Fatal("checksum failure left buffered state behind")
	}

	begin = transferMessage(t, protocol.TypeTransferBegin, "session-3", protocol.TransferBegin{
		TransferID: "transfer-3", Direction: "from_runner", TotalBytes: 4,
	})
	if err := connection.handleTransferMessage(begin); err != nil {
		t.Fatal(err)
	}
	failed := transferMessage(t, protocol.TypeTransferFailed, "session-3", protocol.TransferFailed{
		TransferID: "transfer-3", Code: "workspace_failed", Message: "workspace snapshot failed",
	})
	if err := connection.handleTransferMessage(failed); err == nil || !strings.Contains(err.Error(), "workspace snapshot failed") {
		t.Fatalf("remote transfer failure error=%v", err)
	}
	if _, ok := connection.transfers["session-3"]; ok {
		t.Fatal("remote transfer failure left buffered state behind")
	}
}

func TestTransferMessagesFenceMismatchedTransferID(t *testing.T) {
	connection := newTransferStateConnection()
	payload := []byte("abc")
	begin := transferMessage(t, protocol.TypeTransferBegin, "session-1", protocol.TransferBegin{
		TransferID: "current", Direction: "from_runner", TotalBytes: int64(len(payload)), Checksum: protocol.TransferChecksum(payload),
	})
	if err := connection.handleTransferMessage(begin); err != nil {
		t.Fatal(err)
	}
	mismatchedChunk := transferMessage(t, protocol.TypeTransferChunk, "session-1", protocol.TransferChunk{
		TransferID: "stale", Data: base64.StdEncoding.EncodeToString(payload),
	})
	if err := connection.handleTransferMessage(mismatchedChunk); err != nil {
		t.Fatal(err)
	}
	mismatchedEnd := transferMessage(t, protocol.TypeTransferEnd, "session-1", protocol.TransferEnd{TransferID: "stale"})
	if err := connection.handleTransferMessage(mismatchedEnd); err != nil {
		t.Fatal(err)
	}
	if got := len(connection.transfers["session-1"].buffer); got != 0 {
		t.Fatalf("stale transfer mutated current buffer: %d bytes", got)
	}

	matchingChunk := transferMessage(t, protocol.TypeTransferChunk, "session-1", protocol.TransferChunk{
		TransferID: "current", Data: base64.StdEncoding.EncodeToString(payload),
	})
	if err := connection.handleTransferMessage(matchingChunk); err != nil {
		t.Fatal(err)
	}
	matchingEnd := transferMessage(t, protocol.TypeTransferEnd, "session-1", protocol.TransferEnd{TransferID: "current"})
	if err := connection.handleTransferMessage(matchingEnd); err != nil {
		t.Fatal(err)
	}
	transferID, received, err := connection.ReceiveTransfer(context.Background(), "session-1", nil)
	if err != nil || transferID != "current" || string(received) != string(payload) {
		t.Fatalf("current transfer id=%q payload=%q err=%v", transferID, received, err)
	}
}

func TestTransferProtocolRejectsMalformedMessagesAndCancelledAcknowledgement(t *testing.T) {
	connection := newTransferStateConnection()
	if err := connection.handleTransferMessage(protocol.Message{Version: protocol.Version2, Type: protocol.TypeTransferBegin}); err == nil {
		t.Fatal("transfer message without session id was accepted")
	}

	invalidBegin := transferMessage(t, protocol.TypeTransferBegin, "session-1", protocol.TransferBegin{Direction: "from_runner"})
	if err := connection.handleTransferMessage(invalidBegin); err == nil {
		t.Fatal("transfer begin without transfer id was accepted")
	}

	malformedChunk := transferMessage(t, protocol.TypeTransferChunk, "session-1", protocol.TransferChunk{TransferID: "transfer-1"})
	malformedChunk.Payload = json.RawMessage(`{"transfer_id":`)
	if err := connection.handleTransferMessage(malformedChunk); err == nil {
		t.Fatal("malformed transfer payload was accepted")
	}

	if err := connection.handleTransferMessage(protocol.Message{Version: protocol.Version2, Type: protocol.TypeHealth, SessionID: "session-1"}); err == nil {
		t.Fatal("unexpected transfer message type was accepted")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := connection.ConfirmTransferApplied(ctx, "session-1", "transfer-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acknowledgement error=%v", err)
	}
	if err := (*Connection)(nil).ConfirmTransferApplied(context.Background(), "session-1", "transfer-1"); err == nil {
		t.Fatal("nil connection acknowledgement was accepted")
	}
	if err := connection.ConfirmTransferApplied(context.Background(), " ", "transfer-1"); err == nil {
		t.Fatal("blank session acknowledgement was accepted")
	}
	if _, _, err := (*Connection)(nil).ReceiveTransfer(context.Background(), "session-1", nil); err == nil {
		t.Fatal("nil connection receive was accepted")
	}
}
