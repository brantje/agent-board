package store

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestIssueBoardMutationEvents(t *testing.T) {
	creatorType := ActorTypeHuman
	creatorID := "user-1"
	issue := Issue{
		ID:            "issue-1",
		ProjectID:     "project-1",
		Title:         "Ordered work",
		Status:        "TODO",
		Priority:      3,
		BoardPosition: 4,
		CreatedByType: &creatorType,
		CreatedByID:   &creatorID,
	}

	created, err := NewIssueCreatedEvent(issue)
	if err != nil {
		t.Fatalf("NewIssueCreatedEvent: %v", err)
	}
	assertIssueEvent(t, created, "issue.created", issue, map[string]string{"type": ActorTypeHuman, "id": creatorID}, "")

	actor := json.RawMessage(`{"type":"HUMAN","id":"user-2"}`)
	updated, err := NewIssueUpdatedEventWithActor(issue, "TODO", actor)
	if err != nil {
		t.Fatalf("NewIssueUpdatedEventWithActor same status: %v", err)
	}
	assertIssueEvent(t, updated, "issue.updated", issue, map[string]string{"type": ActorTypeHuman, "id": "user-2"}, "")

	changed, err := NewIssueUpdatedEventWithActor(issue, "BACKLOG", nil)
	if err != nil {
		t.Fatalf("NewIssueUpdatedEventWithActor status change: %v", err)
	}
	assertIssueEvent(t, changed, "issue.status_changed", issue, map[string]string{}, "BACKLOG")
}

func TestValidIssueStatus(t *testing.T) {
	for _, status := range []string{"BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE"} {
		if !ValidIssueStatus(status) {
			t.Errorf("ValidIssueStatus(%q) = false, want true", status)
		}
	}
	for _, status := range []string{"", "QUEUED", "done"} {
		if ValidIssueStatus(status) {
			t.Errorf("ValidIssueStatus(%q) = true, want false", status)
		}
	}
}

func assertIssueEvent(t *testing.T, event Event, eventType string, issue Issue, wantActor map[string]string, previousStatus string) {
	t.Helper()
	if event.Type != eventType {
		t.Fatalf("event type = %q, want %q", event.Type, eventType)
	}
	if event.ProjectID != issue.ProjectID || event.IssueID == nil || *event.IssueID != issue.ID {
		t.Fatalf("event scope = project %q issue %v, want project %q issue %q", event.ProjectID, event.IssueID, issue.ProjectID, issue.ID)
	}

	var actor map[string]string
	if err := json.Unmarshal(event.Actor, &actor); err != nil {
		t.Fatalf("decode actor: %v", err)
	}
	if !reflect.DeepEqual(actor, wantActor) {
		t.Fatalf("actor = %#v, want %#v", actor, wantActor)
	}

	var payload struct {
		Title          string `json:"title"`
		Status         string `json:"status"`
		Priority       int    `json:"priority"`
		BoardPosition  int64  `json:"boardPosition"`
		PreviousStatus string `json:"previousStatus"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Title != issue.Title || payload.Status != issue.Status || payload.Priority != issue.Priority || payload.BoardPosition != issue.BoardPosition {
		t.Fatalf("payload = %#v, want issue fields %#v", payload, issue)
	}
	if payload.PreviousStatus != previousStatus {
		t.Fatalf("previousStatus = %q, want %q", payload.PreviousStatus, previousStatus)
	}
}
