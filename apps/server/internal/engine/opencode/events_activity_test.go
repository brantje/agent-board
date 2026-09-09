package opencode

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
)

type failFirstActivitySink struct {
	failed bool
	events []engine.ActivityEvent
}

func (s *failFirstActivitySink) RecordActivity(_ context.Context, event engine.ActivityEvent) error {
	if !s.failed {
		s.failed = true
		return errors.New("persist failed")
	}
	s.events = append(s.events, event)
	return nil
}

func TestHandleReasoningPartPersistsFinalVisibleReasoningOnce(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)

	parts := []map[string]any{
		{"id": "reason_partial", "sessionID": "ses_1", "text": "partial", "time": map[string]any{"start": 1}},
		{"id": "reason_ignored", "sessionID": "ses_1", "text": "ignored", "ignored": true, "time": map[string]any{"end": 2}},
		{"id": "reason_1", "sessionID": "ses_1", "text": "Check the handlers first", "time": map[string]any{"end": 3}},
		{"id": "reason_1", "sessionID": "ses_1", "text": "Check the handlers first", "time": map[string]any{"end": 3}},
	}
	for _, part := range parts {
		if err := state.handleReasoningPart(context.Background(), mustJSON(t, part)); err != nil {
			t.Fatalf("handleReasoningPart() error=%v", err)
		}
	}
	if len(sink.events) != 1 {
		t.Fatalf("events=%+v", sink.events)
	}
	event := sink.events[0]
	if event.Type != "agent.message" || event.Payload["kind"] != "reasoning" || event.Payload["message"] != "Check the handlers first" || event.Payload["source"] != "opencode" {
		t.Fatalf("event=%+v", event)
	}
}

func TestHandleReasoningPartRetriesPersistenceBeforeDedup(t *testing.T) {
	sink := &failFirstActivitySink{}
	state := newRunState("ses_1", nil, sink)
	part := mustJSON(t, map[string]any{"id": "reason_retry", "sessionID": "ses_1", "text": "Retry me", "time": map[string]any{"end": 1}})
	if err := state.handleReasoningPart(context.Background(), part); err == nil {
		t.Fatal("first persistence unexpectedly succeeded")
	}
	if err := state.handleReasoningPart(context.Background(), part); err != nil {
		t.Fatalf("retry error=%v", err)
	}
	if len(sink.events) != 1 || sink.events[0].Payload["message"] != "Retry me" {
		t.Fatalf("events=%+v", sink.events)
	}
}

func TestHandleToolPartPersistsCallInputAndBoundedResult(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)
	longOutput := strings.Repeat("x", evidence.ActivityPreviewLimit+100)

	for _, part := range []map[string]any{
		{
			"id": "part_1", "callID": "call_1", "tool": "read",
			"state": map[string]any{"status": "running", "title": "Read issue handler", "input": map[string]any{"filePath": "server/internal/handler/issue.go"}},
		},
		{
			"id": "part_1", "callID": "call_1", "tool": "read",
			"state": map[string]any{"status": "completed", "title": "Read issue handler", "input": map[string]any{"filePath": "server/internal/handler/issue.go"}, "output": longOutput},
		},
	} {
		if err := state.handleToolPart(context.Background(), mustJSON(t, part)); err != nil {
			t.Fatalf("handleToolPart() error=%v", err)
		}
	}
	if len(sink.events) != 2 {
		t.Fatalf("events=%+v", sink.events)
	}
	started := sink.events[0].Payload
	if started["toolCallId"] != "call_1" || started["source"] != "opencode" {
		t.Fatalf("started=%+v", started)
	}
	input, ok := started["input"].(map[string]any)
	if !ok || input["filePath"] != "server/internal/handler/issue.go" {
		t.Fatalf("input=%+v", started["input"])
	}
	preview, _ := sink.events[1].Payload["resultPreview"].(string)
	if len(preview) > evidence.ActivityPreviewLimit || !strings.HasSuffix(preview, "…") {
		t.Fatalf("preview length=%d suffix=%q", len(preview), preview[len(preview)-3:])
	}
}

var _ engine.ActivitySink = (*failFirstActivitySink)(nil)
