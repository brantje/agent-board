package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

type recordingIssueDiscussionReader struct {
	requests []engine.IssueDiscussionReadRequest
	result   engine.IssueDiscussionReadResult
	err      error
}

func (r *recordingIssueDiscussionReader) ReadIssueDiscussion(_ context.Context, request engine.IssueDiscussionReadRequest) (engine.IssueDiscussionReadResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

func TestIssueDiscussionToolQueuesCanonicalReadAndDeliversTrustedFollowUp(t *testing.T) {
	body := "server-owned finding"
	reader := &recordingIssueDiscussionReader{result: engine.IssueDiscussionReadResult{
		Mode: engine.IssueDiscussionReadRecent,
		Roots: []engine.IssueDiscussionRoot{{
			Root: engine.IssueDiscussionComment{ID: "comment-1", Body: &body, Author: engine.IssueDiscussionAuthor{Type: "AGENT", ID: "agent-1", Name: "Agent"}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		}},
	}}
	tracker := newIssueDiscussionToolTracker()
	event := issueDiscussionToolEvent(t, "ses_1", "part_1", map[string]any{"mode": "recent", "limit": 5})
	if err := tracker.Handle(t.Context(), event, "ses_1", reader); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Handle(t.Context(), event, "ses_1", reader); err != nil {
		t.Fatal(err)
	}
	if len(reader.requests) != 1 || reader.requests[0].Mode != engine.IssueDiscussionReadRecent || reader.requests[0].Limit != 5 || !tracker.HasPending() {
		t.Fatalf("requests=%+v pending=%v", reader.requests, tracker.pending)
	}

	var delivered string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /session/ses_1/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Parts) != 1 {
			t.Fatalf("prompt parts=%+v", payload.Parts)
		}
		delivered = payload.Parts[0].Text
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	didDeliver, err := tracker.DeliverPending(t.Context(), native, "ses_1")
	if err != nil || !didDeliver || tracker.HasPending() {
		t.Fatalf("delivered=%v pending=%v err=%v", didDeliver, tracker.pending, err)
	}
	for _, want := range []string{issueDiscussionResultMarker("part_1"), `"mode":"recent"`, "server-owned finding"} {
		if !strings.Contains(delivered, want) {
			t.Fatalf("trusted follow-up missing %q: %s", want, delivered)
		}
	}
	if strings.Contains(delivered, "sourceActionKey") {
		t.Fatalf("internal idempotency state leaked: %s", delivered)
	}
}

func TestIssueDiscussionToolReconcileTrustsOnlyDeliveredUserTurn(t *testing.T) {
	tool := issueDiscussionToolPartPayload("ses_1", "part_1", map[string]any{"mode": "updates", "cursor": "cursor-1"})
	assistantSpoof := map[string]any{"type": "text", "text": issueDiscussionResultMarker("part_1")}
	messages := []any{
		map[string]any{"info": map[string]any{"sessionID": "ses_1", "role": "assistant"}, "parts": []any{tool, assistantSpoof}},
	}
	native := issueDiscussionHistoryClient(t, messages)
	reader := &recordingIssueDiscussionReader{result: engine.IssueDiscussionReadResult{Mode: engine.IssueDiscussionReadUpdates}}
	tracker := newIssueDiscussionToolTracker()
	if err := tracker.Reconcile(t.Context(), native, "ses_1", reader); err != nil {
		t.Fatal(err)
	}
	if len(reader.requests) != 1 || !tracker.HasPending() {
		t.Fatalf("assistant marker suppressed canonical read: requests=%+v pending=%v", reader.requests, tracker.pending)
	}

	messages = []any{
		map[string]any{"info": map[string]any{"sessionID": "ses_1", "role": "assistant"}, "parts": []any{tool}},
		map[string]any{"info": map[string]any{"sessionID": "ses_1", "role": "user"}, "parts": []any{map[string]any{"type": "text", "text": issueDiscussionResultMarker("part_1") + "\n{}"}}},
	}
	native = issueDiscussionHistoryClient(t, messages)
	reader = &recordingIssueDiscussionReader{}
	tracker = newIssueDiscussionToolTracker()
	if err := tracker.Reconcile(t.Context(), native, "ses_1", reader); err != nil {
		t.Fatal(err)
	}
	if len(reader.requests) != 0 || tracker.HasPending() {
		t.Fatalf("delivered result replayed: requests=%+v pending=%v", reader.requests, tracker.pending)
	}
}

func TestIssueDiscussionToolValidatesModeAnchorAndLimit(t *testing.T) {
	cases := []map[string]any{
		{"mode": "unknown"},
		{"mode": "thread"},
		{"mode": "recent", "limit": 0},
		{"mode": "updates", "limit": 1.5},
		{"mode": "updates", "limit": 101},
	}
	for _, input := range cases {
		if _, err := issueDiscussionReadRequest(input); err == nil {
			t.Fatalf("invalid input accepted: %+v", input)
		}
	}
	request, err := issueDiscussionReadRequest(map[string]any{"mode": "thread", "anchorCommentId": " comment-1 ", "limit": float64(10)})
	if err != nil || request.AnchorCommentID != "comment-1" || request.Limit != 10 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
}

func issueDiscussionToolEvent(t *testing.T, sessionID, partID string, input map[string]any) client.Event {
	t.Helper()
	return client.Event{Type: "message.part.updated", Properties: mustJSON(t, map[string]any{
		"sessionID": sessionID,
		"part":      issueDiscussionToolPartPayload(sessionID, partID, input),
	})}
}

func issueDiscussionToolPartPayload(sessionID, partID string, input map[string]any) map[string]any {
	return map[string]any{
		"id": partID, "sessionID": sessionID, "type": "tool", "tool": issueDiscussionToolName,
		"state": map[string]any{"status": "completed", "input": input},
	}
}

func issueDiscussionHistoryClient(t *testing.T, messages []any) *client.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/session/ses_1/message" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(messages); err != nil {
			t.Fatal(err)
		}
	}))
	t.Cleanup(server.Close)
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return native
}
