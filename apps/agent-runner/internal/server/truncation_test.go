package server

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	"github.com/brantje/agent-board/apps/agent-runner/internal/session"
)

type recordingStreamWriter struct {
	messageTypes []protocol.MessageType
	errors       []protocol.ErrorPayload
}

func (w *recordingStreamWriter) send(typ protocol.MessageType, _ string, _ any) error {
	w.messageTypes = append(w.messageTypes, typ)
	return nil
}

func (w *recordingStreamWriter) sendError(code, message, _ string) {
	w.errors = append(w.errors, protocol.ErrorPayload{Code: code, Message: message})
}

func TestPumpStreamReportsTruncatedOutput(t *testing.T) {
	manager := session.NewManagerWithWorkspace(1, t.TempDir())
	execution, err := manager.Start("truncated", session.Request{
		Command: []string{"sh", "-c", "dd if=/dev/zero bs=65536 count=32 2>/dev/null"},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := execution.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	writer := &recordingStreamWriter{}
	pumpStream(writer, protocol.TypeStdout, execution.ID(), execution.Stdout())

	if len(writer.messageTypes) == 0 {
		t.Fatal("expected retained stdout to be sent before truncation notification")
	}
	for _, typ := range writer.messageTypes {
		if typ != protocol.TypeStdout {
			t.Fatalf("unexpected stream message type %q", typ)
		}
	}
	if len(writer.errors) != 1 {
		t.Fatalf("expected one truncation error, got %#v", writer.errors)
	}
	if got := writer.errors[0]; got.Code != "output_truncated" || got.Message != "session output was truncated" {
		t.Fatalf("unexpected truncation error %#v", got)
	}
}
