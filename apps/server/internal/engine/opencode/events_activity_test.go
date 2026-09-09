package opencode

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
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

func TestFlushPendingMessagesPreservesArrivalOrder(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)
	first := mustJSON(t, map[string]any{"id": "msg_a", "sessionID": "ses_1", "text": "First buffered message", "time": map[string]any{"start": 1}})
	second := mustJSON(t, map[string]any{"id": "reason_b", "sessionID": "ses_1", "text": "Second buffered reasoning", "time": map[string]any{"start": 2}})
	if err := state.handleTextPart(context.Background(), first); err != nil {
		t.Fatalf("buffer first message error=%v", err)
	}
	if err := state.handleReasoningPart(context.Background(), second); err != nil {
		t.Fatalf("buffer second reasoning error=%v", err)
	}
	if err := state.flushPendingMessages(context.Background()); err != nil {
		t.Fatalf("flush pending messages error=%v", err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events=%+v", sink.events)
	}
	if sink.events[0].Payload["kind"] != "message" || sink.events[0].Payload["message"] != "First buffered message" {
		t.Fatalf("first flushed event=%+v", sink.events[0])
	}
	if sink.events[1].Payload["kind"] != "reasoning" || sink.events[1].Payload["message"] != "Second buffered reasoning" {
		t.Fatalf("second flushed event=%+v", sink.events[1])
	}
}

func TestPendingReasoningFlushesBeforeToolAndIdle(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)
	reasoning := mustJSON(t, map[string]any{"id": "reason_open", "sessionID": "ses_1", "text": "Node is missing. Try apk.", "time": map[string]any{"start": 1}})
	if err := state.handleReasoningPart(context.Background(), reasoning); err != nil {
		t.Fatalf("buffer reasoning error=%v", err)
	}
	if len(sink.events) != 0 {
		t.Fatalf("incomplete reasoning persisted early: %+v", sink.events)
	}

	tool := mustJSON(t, map[string]any{
		"id": "tool_1", "tool": "bash",
		"state": map[string]any{"status": "running", "title": "which apk", "output": ""},
	})
	if err := state.handleToolPart(context.Background(), tool); err != nil {
		t.Fatalf("flush before tool error=%v", err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events=%+v", sink.events)
	}
	if sink.events[0].Type != "agent.message" || sink.events[0].Payload["kind"] != "reasoning" || sink.events[0].Payload["message"] != "Node is missing. Try apk." {
		t.Fatalf("flushed reasoning=%+v", sink.events[0])
	}
	if sink.events[1].Type != "tool.started" {
		t.Fatalf("tool=%+v", sink.events[1])
	}

	followUp := mustJSON(t, map[string]any{"id": "txt_idle", "sessionID": "ses_1", "text": "Switch to static HTML.", "time": map[string]any{"start": 2}})
	if err := state.handleTextPart(context.Background(), followUp); err != nil {
		t.Fatalf("buffer text error=%v", err)
	}
	if err := state.handleEvent(context.Background(), nil, client.Event{Type: "session.idle", Properties: mustJSON(t, map[string]any{"sessionID": "ses_1"})}); err != nil {
		t.Fatalf("idle flush error=%v", err)
	}
	if len(sink.events) != 3 || sink.events[2].Payload["kind"] != "message" || sink.events[2].Payload["message"] != "Switch to static HTML." {
		t.Fatalf("idle events=%+v", sink.events)
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

func TestHandleToolPartPreservesStructuredOutputAsPreview(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)
	part := mustJSON(t, map[string]any{
		"id": "part_structured", "callID": "call_structured", "tool": "search",
		"state": map[string]any{
			"status": "completed",
			"output": map[string]any{"matches": []any{"a.go", "b.go"}, "count": 2},
		},
	})
	if err := state.handleToolPart(context.Background(), part); err != nil {
		t.Fatalf("handleToolPart() error=%v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events=%+v", sink.events)
	}
	preview, _ := sink.events[0].Payload["resultPreview"].(string)
	if preview != `{"count":2,"matches":["a.go","b.go"]}` {
		t.Fatalf("structured preview=%q", preview)
	}
}

var _ engine.ActivitySink = (*failFirstActivitySink)(nil)
