package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueLastEventSkipsToolChatterAndIsolatesProjects(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()
	f := seedRunFixture(t, s, "last-event")
	other := seedRunFixture(t, s, "last-event-other")

	if _, err := s.AppendEvent(ctx, store.Event{
		ProjectID: f.project.ID, IssueID: &f.issue.ID, Type: "issue.created",
		Payload: json.RawMessage(`{"status":"TODO"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(ctx, store.Event{
		ProjectID: f.project.ID, IssueID: &f.issue.ID, RunID: &f.run.ID, Type: "tool.completed",
		Payload: json.RawMessage(`{"name":"edit"}`),
	}); err != nil {
		t.Fatal(err)
	}
	question, err := s.AppendEvent(ctx, store.Event{
		ProjectID: f.project.ID, IssueID: &f.issue.ID, RunID: &f.run.ID, Type: "question.created",
		OccurredAt: time.Now().UTC().Add(time.Second),
		Payload:    json.RawMessage(`{"prompt":"Choose"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(ctx, store.Event{
		ProjectID: other.project.ID, IssueID: &other.issue.ID, Type: "issue.created",
	}); err != nil {
		t.Fatal(err)
	}

	listed, err := s.ListIssues(ctx, f.project.ID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list=%+v err=%v", listed, err)
	}
	if listed[0].LastEvent == nil || listed[0].LastEvent.ID != question.ID || listed[0].LastEvent.Type != "question.created" {
		t.Fatalf("list lastEvent=%+v want %s", listed[0].LastEvent, question.ID)
	}

	got, err := s.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil || got.LastEvent == nil || got.LastEvent.ID != question.ID {
		t.Fatalf("get lastEvent=%+v err=%v", got.LastEvent, err)
	}

	foreign, err := s.ListIssues(ctx, other.project.ID)
	if err != nil || len(foreign) != 1 || foreign[0].LastEvent == nil || foreign[0].LastEvent.Type != "issue.created" {
		t.Fatalf("foreign lastEvent=%+v err=%v", foreign, err)
	}
	if foreign[0].LastEvent.ID == question.ID {
		t.Fatal("leaked lastEvent across projects")
	}
}
