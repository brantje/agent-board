package opencode

import (
	"context"
	"strings"
	"testing"
)

func TestHandleTextPartHidesOnlyIssueStatusPromptSubstring(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)
	prefix := "Issue: Keep activity focused\n\n"
	suffix := "\n\nWork directly in the current project directory."
	part := mustJSON(t, map[string]any{
		"id":        "msg_prompt",
		"sessionID": "ses_1",
		"text":      prefix + issueStatusActivityHiddenSubstring + suffix,
		"time":      map[string]any{"end": 1},
	})

	if err := state.handleTextPart(context.Background(), part); err != nil {
		t.Fatalf("handleTextPart() error=%v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events=%+v", sink.events)
	}
	message, _ := sink.events[0].Payload["message"].(string)
	if message != prefix+suffix {
		t.Fatalf("message=%q want=%q", message, prefix+suffix)
	}
	if strings.Contains(message, issueStatusActivityHiddenSubstring) {
		t.Fatalf("status guidance leaked into activity: %q", message)
	}
}

func TestHandleTextPartDropsIssueStatusPromptOnlyActivity(t *testing.T) {
	sink := &recordingActivitySink{}
	state := newRunState("ses_1", nil, sink)
	part := mustJSON(t, map[string]any{
		"id":        "msg_prompt_only",
		"sessionID": "ses_1",
		"text":      issueStatusActivityHiddenSubstring,
		"time":      map[string]any{"end": 1},
	})

	if err := state.handleTextPart(context.Background(), part); err != nil {
		t.Fatalf("handleTextPart() error=%v", err)
	}
	if len(sink.events) != 0 {
		t.Fatalf("events=%+v", sink.events)
	}
}
