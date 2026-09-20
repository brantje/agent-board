package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

type recordingIssueCommentPublisher struct {
	requests []engine.IssueCommentPublishRequest
	err      error
}

func (p *recordingIssueCommentPublisher) PublishIssueComment(_ context.Context, request engine.IssueCommentPublishRequest) (engine.PublishedIssueComment, error) {
	p.requests = append(p.requests, request)
	if p.err != nil {
		return engine.PublishedIssueComment{}, p.err
	}
	return engine.PublishedIssueComment{ID: "comment-1"}, nil
}

func TestIssueCommentToolCompletionPublishesOnce(t *testing.T) {
	tracker := newIssueCommentToolTracker()
	publisher := &recordingIssueCommentPublisher{}
	event := issueCommentToolEvent(t, "ses_1", "part_1", "  concise finding  ", "completed")

	if err := tracker.Handle(t.Context(), event, "ses_1", publisher); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Handle(t.Context(), event, "ses_1", publisher); err != nil {
		t.Fatal(err)
	}
	if len(publisher.requests) != 1 || publisher.requests[0].Body != "concise finding" || publisher.requests[0].RequestKey != "part_1" {
		t.Fatalf("requests=%+v", publisher.requests)
	}
}

func TestIssueCommentToolReconcileRecoversDurableCompletions(t *testing.T) {
	native := issueStatusHistoryClient(t, "ses_1", []any{
		map[string]any{
			"info": map[string]any{"sessionID": "ses_1"},
			"parts": []any{
				issueCommentToolPartPayload("ses_1", "part_1", "first", "completed"),
				issueCommentToolPartPayload("ses_1", "part_2", "second", "completed"),
			},
		},
	})
	tracker := newIssueCommentToolTracker()
	publisher := &recordingIssueCommentPublisher{}

	if err := tracker.Reconcile(t.Context(), native, "ses_1", publisher); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Reconcile(t.Context(), native, "ses_1", publisher); err != nil {
		t.Fatal(err)
	}
	if len(publisher.requests) != 2 || publisher.requests[0].RequestKey != "part_1" || publisher.requests[1].RequestKey != "part_2" {
		t.Fatalf("requests=%+v", publisher.requests)
	}
}

func TestIssueCommentToolIgnoresOtherSessionAndNonCompletedParts(t *testing.T) {
	tracker := newIssueCommentToolTracker()
	publisher := &recordingIssueCommentPublisher{}
	for _, event := range []client.Event{
		issueCommentToolEvent(t, "ses_other", "part_other", "ignored", "completed"),
		issueCommentToolEvent(t, "ses_1", "part_running", "ignored", "running"),
	} {
		if err := tracker.Handle(t.Context(), event, "ses_1", publisher); err != nil {
			t.Fatal(err)
		}
	}
	if len(publisher.requests) != 0 {
		t.Fatalf("requests=%+v", publisher.requests)
	}
}

func TestIssueCommentToolValidatesAndRetriesFailedPublication(t *testing.T) {
	tracker := newIssueCommentToolTracker()
	if err := tracker.Handle(t.Context(), issueCommentToolEvent(t, "ses_1", "", "body", "completed"), "ses_1", &recordingIssueCommentPublisher{}); err == nil {
		t.Fatal("missing part id unexpectedly accepted")
	}
	if err := tracker.Handle(t.Context(), issueCommentToolEvent(t, "ses_1", "part_blank", "   ", "completed"), "ses_1", &recordingIssueCommentPublisher{}); err == nil {
		t.Fatal("blank body unexpectedly accepted")
	}

	publisher := &recordingIssueCommentPublisher{err: errors.New("persistence unavailable")}
	event := issueCommentToolEvent(t, "ses_1", "part_retry", "finding", "completed")
	for attempt := 0; attempt < 2; attempt++ {
		err := tracker.Handle(t.Context(), event, "ses_1", publisher)
		if err == nil || !strings.Contains(err.Error(), "persistence unavailable") {
			t.Fatalf("attempt %d error=%v", attempt+1, err)
		}
	}
	if len(publisher.requests) != 2 {
		t.Fatalf("failed publication was marked seen: %+v", publisher.requests)
	}
}

func issueCommentToolEvent(t *testing.T, sessionID, partID, body, status string) client.Event {
	t.Helper()
	properties, err := json.Marshal(map[string]any{
		"sessionID": sessionID,
		"part": issueCommentToolPartPayload(sessionID, partID, body, status),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client.Event{Type: "message.part.updated", Properties: properties}
}

func issueCommentToolPartPayload(sessionID, partID, body, status string) map[string]any {
	return map[string]any{
		"id": partID, "sessionID": sessionID, "type": "tool", "tool": issueCommentToolName,
		"state": map[string]any{"status": status, "input": map[string]any{"body": body}},
	}
}
