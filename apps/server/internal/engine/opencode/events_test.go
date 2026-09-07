package opencode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

type recordingActivitySink struct {
	events []engine.ActivityEvent
}

func (s *recordingActivitySink) RecordActivity(_ context.Context, event engine.ActivityEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestHandlePartUpdatedRecordsOnlyFinalVisibleTextOnce(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)

	updates := []map[string]any{
		{"sessionID": "other", "part": map[string]any{"id": "txt_other", "sessionID": "other", "type": "text", "text": "ignore", "time": map[string]any{"end": 1}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "txt_1", "sessionID": "ses_1", "type": "text", "text": "partial", "time": map[string]any{"start": 1}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "txt_ignored", "sessionID": "ses_1", "type": "text", "text": "hidden", "ignored": true, "time": map[string]any{"end": 2}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "txt_1", "sessionID": "ses_1", "type": "text", "text": "final answer", "time": map[string]any{"end": 3}}},
		{"sessionID": "ses_1", "part": map[string]any{"id": "txt_1", "sessionID": "ses_1", "type": "text", "text": "final answer", "time": map[string]any{"end": 3}}},
	}
	for _, update := range updates {
		if err := state.handlePartUpdated(context.Background(), mustJSON(t, update)); err != nil {
			t.Fatalf("handlePartUpdated() error=%v", err)
		}
	}

	if state.lastVisibleMessage != "final answer" {
		t.Fatalf("lastVisibleMessage=%q", state.lastVisibleMessage)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events=%v", sink.events)
	}
	if sink.events[0].Type != "agent.message" || sink.events[0].Payload["message"] != "final answer" || sink.events[0].Payload["source"] != "opencode" {
		t.Fatalf("event=%+v", sink.events[0])
	}
}

func TestHandlePartUpdatedDropsReasoningAndWrongPartSession(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)

	for _, part := range []map[string]any{
		{"id": "reason_1", "sessionID": "ses_1", "type": "reasoning", "text": "private reasoning"},
		{"id": "txt_other", "sessionID": "other", "type": "text", "text": "wrong session", "time": map[string]any{"end": 1}},
		{"id": "unknown_1", "sessionID": "ses_1", "type": "snapshot"},
	} {
		properties := mustJSON(t, map[string]any{"sessionID": "ses_1", "part": part})
		if err := state.handleEvent(context.Background(), nil, client.Event{Type: "message.part.updated", Properties: properties}); err != nil {
			t.Fatalf("handleEvent() error=%v", err)
		}
	}
	if len(sink.events) != 0 || state.lastVisibleMessage != "" {
		t.Fatalf("reasoning or unrelated part leaked: events=%v message=%q", sink.events, state.lastVisibleMessage)
	}
}

func TestHandleToolPartMapsLifecycleAndDeduplicates(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)

	parts := []map[string]any{
		{"id": "tool_1", "tool": "bash", "state": map[string]any{"status": "running", "title": "Run tests"}},
		{"id": "tool_1", "tool": "bash", "state": map[string]any{"status": "running", "title": "Run tests"}},
		{"id": "tool_1", "tool": "bash", "state": map[string]any{"status": "completed", "title": "Run tests"}},
		{"id": "tool_2", "tool": "edit", "state": map[string]any{"status": "error", "error": "write failed"}},
		{"id": "tool_3", "tool": "edit", "state": map[string]any{"status": "pending"}},
		{"id": "tool_4", "tool": "", "state": map[string]any{"status": "running"}},
	}
	for _, part := range parts {
		if err := state.handleToolPart(context.Background(), mustJSON(t, part)); err != nil {
			t.Fatalf("handleToolPart() error=%v", err)
		}
	}

	if len(sink.events) != 3 {
		t.Fatalf("events=%v", sink.events)
	}
	if sink.events[0].Type != "tool.started" || sink.events[0].Payload["summary"] != "Run tests" {
		t.Fatalf("started=%+v", sink.events[0])
	}
	if sink.events[1].Type != "tool.completed" {
		t.Fatalf("completed=%+v", sink.events[1])
	}
	if sink.events[2].Type != "tool.failed" || sink.events[2].Payload["reason"] != "write failed" {
		t.Fatalf("failed=%+v", sink.events[2])
	}
}

func TestNativeEventDecodersRejectMalformedPayloads(t *testing.T) {
	state := newRunState("ses_1", nil, nil)
	if err := state.handleEvent(context.Background(), nil, client.Event{Type: "message.part.updated", Properties: json.RawMessage("{")}); err == nil {
		t.Fatal("malformed part update unexpectedly accepted")
	}
	if err := state.handleEvent(context.Background(), nil, client.Event{Type: "question.v2.asked", Properties: json.RawMessage("{")}); err == nil {
		t.Fatal("malformed Question event unexpectedly accepted")
	}
	if err := state.handleTextPart(context.Background(), json.RawMessage("{")); err == nil {
		t.Fatal("malformed text part unexpectedly accepted")
	}
	if err := state.handleToolPart(context.Background(), json.RawMessage("{")); err == nil {
		t.Fatal("malformed tool part unexpectedly accepted")
	}
	if err := state.handleEvent(context.Background(), nil, client.Event{Type: "server.connected", Properties: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("unknown native event should be ignored: %v", err)
	}
}

func TestActivityHandlersWorkWithoutSink(t *testing.T) {
	state := newRunState("ses_1", nil, nil)
	if err := state.handleTextPart(context.Background(), mustJSON(t, map[string]any{
		"id": "txt_1", "sessionID": "ses_1", "text": "visible", "time": map[string]any{"end": 1},
	})); err != nil {
		t.Fatalf("handleTextPart() error=%v", err)
	}
	if err := state.handleToolPart(context.Background(), mustJSON(t, map[string]any{
		"id": "tool_1", "tool": "bash", "state": map[string]any{"status": "running"},
	})); err != nil {
		t.Fatalf("handleToolPart() error=%v", err)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

var _ engine.ActivitySink = (*recordingActivitySink)(nil)
