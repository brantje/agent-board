package opencode

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

type failOnceActivitySink struct {
	calls  int
	events []engine.ActivityEvent
}

func (s *failOnceActivitySink) RecordActivity(_ context.Context, event engine.ActivityEvent) error {
	s.calls++
	if s.calls == 1 {
		return errors.New("injected activity persistence failure")
	}
	s.events = append(s.events, event)
	return nil
}

func TestTextPartReplayPersistsAfterTransientActivityFailure(t *testing.T) {
	sink := &failOnceActivitySink{}
	state := newRunState("ses_1", nil, sink)
	part := mustJSON(t, map[string]any{
		"id":        "txt_retry",
		"sessionID": "ses_1",
		"text":      "visible after retry",
		"time":      map[string]any{"end": 1},
	})

	if err := state.handleTextPart(context.Background(), part); err == nil {
		t.Fatal("first text persistence unexpectedly succeeded")
	}
	if _, seen := state.seenTextParts["txt_retry"]; seen {
		t.Fatal("failed text persistence was committed to dedup state")
	}
	if state.lastVisibleMessage != "" {
		t.Fatalf("lastVisibleMessage=%q after failed persistence", state.lastVisibleMessage)
	}

	if err := state.handleTextPart(context.Background(), part); err != nil {
		t.Fatalf("replayed text persistence error=%v", err)
	}
	if err := state.handleTextPart(context.Background(), part); err != nil {
		t.Fatalf("deduplicated text replay error=%v", err)
	}
	if sink.calls != 2 || len(sink.events) != 1 {
		t.Fatalf("calls=%d events=%v", sink.calls, sink.events)
	}
	if _, seen := state.seenTextParts["txt_retry"]; !seen {
		t.Fatal("successful text persistence was not committed to dedup state")
	}
	if state.lastVisibleMessage != "visible after retry" {
		t.Fatalf("lastVisibleMessage=%q", state.lastVisibleMessage)
	}
}

func TestToolPartReplayPersistsAfterTransientActivityFailure(t *testing.T) {
	sink := &failOnceActivitySink{}
	state := newRunState("ses_1", nil, sink)
	part := mustJSON(t, map[string]any{
		"id":   "tool_retry",
		"tool": "bash",
		"state": map[string]any{
			"status": "completed",
			"title":  "Run checks",
		},
	})

	if err := state.handleToolPart(context.Background(), part); err == nil {
		t.Fatal("first tool persistence unexpectedly succeeded")
	}
	if _, seen := state.seenToolStates["tool_retry/completed"]; seen {
		t.Fatal("failed tool persistence was committed to dedup state")
	}

	if err := state.handleToolPart(context.Background(), part); err != nil {
		t.Fatalf("replayed tool persistence error=%v", err)
	}
	if err := state.handleToolPart(context.Background(), part); err != nil {
		t.Fatalf("deduplicated tool replay error=%v", err)
	}
	if sink.calls != 2 || len(sink.events) != 1 {
		t.Fatalf("calls=%d events=%v", sink.calls, sink.events)
	}
	if _, seen := state.seenToolStates["tool_retry/completed"]; !seen {
		t.Fatal("successful tool persistence was not committed to dedup state")
	}
}

var _ engine.ActivitySink = (*failOnceActivitySink)(nil)
