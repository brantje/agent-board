package runner

import (
	"context"
	"testing"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func TestShouldEmitTransferProgress(t *testing.T) {
	now := time.Unix(100, 0)
	state := &transferProgressState{lastEmitted: now, lastBytes: 10}
	interval := DefaultTransferProgressInterval

	if !shouldEmitTransferProgress(1, 100, nil, now, interval) {
		t.Fatal("expected first progress emission")
	}
	if shouldEmitTransferProgress(14, 100, state, now.Add(100*time.Millisecond), interval) {
		t.Fatal("expected progress to be throttled within interval")
	}
	if !shouldEmitTransferProgress(20, 100, state, now.Add(interval), interval) {
		t.Fatal("expected progress after interval elapsed")
	}
	if !shouldEmitTransferProgress(100, 100, state, now.Add(interval), interval) {
		t.Fatal("expected final progress at completion")
	}

	state.lastBytes = 40
	state.lastEmitted = now
	if !shouldEmitTransferProgress(50, 100, state, now.Add(100*time.Millisecond), interval) {
		t.Fatal("expected meaningful percentage progress emission")
	}
}

func TestSendTransferEmitsThrottledProgress(t *testing.T) {
	server := newProtocolTestServer(t, protocol.Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout", "stderr", "terminate", "kill", "health"}}, func(_ *websocket.Conn, msg protocol.Message) {
		switch msg.Type {
		case protocol.TypeTransferBegin, protocol.TypeTransferChunk, protocol.TypeTransferEnd:
		default:
			t.Errorf("unexpected message type %s", msg.Type)
		}
	})
	defer server.Close()

	conn, err := Dial(context.Background(), wsURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	payload := make([]byte, protocol.TransferChunkSize*100)
	var emissions []TransferProgress
	if err := conn.SendTransfer(context.Background(), "session-1", "transfer-1", "to_runner", payload, func(progress TransferProgress) {
		emissions = append(emissions, progress)
	}); err != nil {
		t.Fatal(err)
	}
	if len(emissions) == 0 {
		t.Fatal("expected transfer progress emissions")
	}
	last := emissions[len(emissions)-1]
	if last.BytesTransferred != int64(len(payload)) || last.TotalBytes != int64(len(payload)) {
		t.Fatalf("unexpected final progress %#v", last)
	}
	if len(emissions) >= len(payload)/protocol.TransferChunkSize {
		t.Fatalf("expected throttled progress emissions, got %d", len(emissions))
	}
}
