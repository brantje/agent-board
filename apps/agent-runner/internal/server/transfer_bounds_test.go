package server

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestIncomingTransferRejectsChecksumMismatchAndOversize(t *testing.T) {
	state := newTransferState()
	begin := protocol.TransferBegin{TransferID: "t1", Direction: "to_runner", TotalBytes: 4, Checksum: "deadbeef"}
	if err := state.begin("session-1", begin); err != nil {
		t.Fatal(err)
	}
	if err := state.chunk("session-1", protocol.TransferChunk{TransferID: "t1", Data: base64.StdEncoding.EncodeToString([]byte("abcd"))}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := state.end("session-1", protocol.TransferEnd{TransferID: "t1"}); err == nil {
		t.Fatal("checksum mismatch accepted")
	}

	oversize := newTransferState()
	if err := oversize.begin("session-1", protocol.TransferBegin{TransferID: "t2", Direction: "to_runner", TotalBytes: 2, Checksum: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := oversize.chunk("session-1", protocol.TransferChunk{TransferID: "t2", Data: base64.StdEncoding.EncodeToString([]byte("abcd"))}); err == nil {
		t.Fatal("oversize chunk accepted")
	}

	empty := newTransferState()
	if err := empty.begin("session-1", protocol.TransferBegin{TransferID: "t3", Direction: "to_runner", TotalBytes: 0}); err != nil {
		t.Fatal(err)
	}
	if err := empty.chunk("session-1", protocol.TransferChunk{TransferID: "t3", Data: base64.StdEncoding.EncodeToString([]byte("x"))}); err == nil {
		t.Fatal("undeclared payload accepted")
	}
	if err := empty.begin("", protocol.TransferBegin{TransferID: "t4", TotalBytes: 1}); err == nil {
		t.Fatal("blank session accepted")
	}
	if err := empty.begin("session-1", protocol.TransferBegin{TotalBytes: 1}); err == nil {
		t.Fatal("blank transfer id accepted")
	}

	ready := newTransferState()
	if ready.isReady("session-1") {
		t.Fatal("session ready before mark")
	}
	ready.markReady("session-1")
	if !ready.isReady("session-1") {
		t.Fatal("session not ready after mark")
	}
	ready.clearReady("session-1")
	if ready.isReady("session-1") {
		t.Fatal("session remained ready after clear")
	}

	mismatch := newTransferState()
	if err := mismatch.begin("session-1", protocol.TransferBegin{TransferID: "t5", Direction: "to_runner", TotalBytes: 4}); err != nil {
		t.Fatal(err)
	}
	if err := mismatch.chunk("session-1", protocol.TransferChunk{TransferID: "other", Data: base64.StdEncoding.EncodeToString([]byte("abcd"))}); err != nil {
		t.Fatal(err)
	}
	payload, direction, err := mismatch.end("session-1", protocol.TransferEnd{TransferID: "other"})
	if err != nil || payload != nil || direction != "" {
		t.Fatalf("mismatched end payload=%q direction=%q err=%v", payload, direction, err)
	}
	if _, _, err := mismatch.end("missing", protocol.TransferEnd{TransferID: "t5"}); err == nil {
		t.Fatal("inactive transfer end accepted")
	}
	if err := mismatch.chunk("missing", protocol.TransferChunk{TransferID: "t5"}); err == nil {
		t.Fatal("inactive transfer chunk accepted")
	}
}

func TestWaitReadyRespectsFailedAndIdleTransfers(t *testing.T) {
	idle := newTransferState()
	if !idle.waitReady("session-1", time.Millisecond) {
		t.Fatal("idle session should be ready immediately")
	}

	failed := newTransferState()
	if err := failed.begin("session-1", protocol.TransferBegin{TransferID: "t1", Direction: "to_runner", TotalBytes: 1}); err != nil {
		t.Fatal(err)
	}
	failed.markFailed("session-1")
	if failed.waitReady("session-1", 40*time.Millisecond) {
		t.Fatal("failed transfer reported ready")
	}

	pending := newTransferState()
	if err := pending.begin("session-1", protocol.TransferBegin{TransferID: "t1", Direction: "to_runner", TotalBytes: 1}); err != nil {
		t.Fatal(err)
	}
	if pending.waitReady("session-1", 20*time.Millisecond) {
		t.Fatal("in-flight transfer reported ready before completion")
	}
	pending.markReady("session-1")
	if !pending.waitReady("session-1", time.Millisecond) {
		t.Fatal("ready transfer was not observed")
	}
}
