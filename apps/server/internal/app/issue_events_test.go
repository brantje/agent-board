package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type capturingIssueRecorder struct {
	events []store.Event
}

func (c *capturingIssueRecorder) Record(_ context.Context, event store.Event) (store.Event, error) {
	event.ID = "evt-" + event.Type
	c.events = append(c.events, event)
	return event, nil
}

func TestIssueMutationsRecordPersistBeforePublishEvents(t *testing.T) {
	pid := coverageProjectID()
	base := &fakeStore{project: store.Project{ID: pid, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, agent: coverageAgent()}
	svc := New(base)
	recorder := &capturingIssueRecorder{}
	svc.SetEventRecorder(recorder)

	created, err := svc.CreateIssue(context.Background(), coverageIssue())
	if err != nil {
		t.Fatal(err)
	}
	if created.LastEvent == nil || created.LastEvent.Type != "issue.created" {
		t.Fatalf("create lastEvent=%+v", created.LastEvent)
	}

	updated := created
	updated.Title = "Renamed"
	got, err := svc.UpdateIssue(context.Background(), updated)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastEvent == nil || got.LastEvent.Type != "issue.updated" {
		t.Fatalf("update lastEvent=%+v", got.LastEvent)
	}

	status := got
	status.Status = "IN_PROGRESS"
	changed, err := svc.UpdateIssue(context.Background(), status)
	if err != nil {
		t.Fatal(err)
	}
	if changed.LastEvent == nil || changed.LastEvent.Type != "issue.status_changed" {
		t.Fatalf("status lastEvent=%+v", changed.LastEvent)
	}

	assigned, _, err := svc.AssignIssue(context.Background(), pid, coverageIssue().ID, coverageAgent().ID)
	if err != nil {
		t.Fatal(err)
	}
	if assigned.LastEvent == nil || assigned.LastEvent.Type != "issue.assigned" {
		t.Fatalf("assign lastEvent=%+v", assigned.LastEvent)
	}

	types := make([]string, 0, len(recorder.events))
	for _, event := range recorder.events {
		types = append(types, event.Type)
		if event.ProjectID != pid || event.IssueID == nil || *event.IssueID != coverageIssue().ID {
			t.Fatalf("event scope=%+v", event)
		}
		if len(event.Payload) == 0 || !json.Valid(event.Payload) {
			t.Fatalf("payload=%s", event.Payload)
		}
	}
	if strings.Join(types, ",") != "issue.created,issue.updated,issue.status_changed,issue.assigned" {
		t.Fatalf("types=%v", types)
	}
}

func TestIssueMutationsSkipEventsWhenRecorderMissing(t *testing.T) {
	pid := coverageProjectID()
	svc := New(&fakeStore{project: store.Project{ID: pid, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, agent: coverageAgent()})
	created, err := svc.CreateIssue(context.Background(), coverageIssue())
	if err != nil || created.LastEvent != nil {
		t.Fatalf("create without recorder=%+v err=%v", created, err)
	}
}
