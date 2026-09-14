package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type capturingIssueRecorder struct {
	events    []store.Event
	published []store.Event
}

func (c *capturingIssueRecorder) Record(_ context.Context, event store.Event) (store.Event, error) {
	event.ID = "evt-" + event.Type
	c.events = append(c.events, event)
	return event, nil
}

func (c *capturingIssueRecorder) PublishPersisted(_ context.Context, event store.Event) {
	c.published = append(c.published, event)
}

func TestIssueMutationsRecordPersistBeforePublishEvents(t *testing.T) {
	pid := coverageProjectID()
	base := &fakeStore{project: store.Project{ID: pid, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, agent: coverageAgent()}
	svc := New(base)
	recorder := &capturingIssueRecorder{}
	svc.SetEventRecorder(recorder)

	creatorType, creatorID := store.ActorTypeAgent, "agent-creator"
	createInput := coverageIssue()
	createInput.CreatedByType, createInput.CreatedByID = &creatorType, &creatorID
	created, err := svc.CreateIssue(context.Background(), createInput)
	if err != nil {
		t.Fatal(err)
	}
	if created.LastEvent == nil || created.LastEvent.Type != "issue.created" {
		t.Fatalf("create lastEvent=%+v", created.LastEvent)
	}
	var createActor map[string]string
	if err := json.Unmarshal(created.LastEvent.Actor, &createActor); err != nil {
		t.Fatal(err)
	}
	if createActor["type"] != store.ActorTypeAgent || createActor["id"] != creatorID {
		t.Fatalf("create actor=%s", created.LastEvent.Actor)
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
	if strings.Join(types, ",") != "issue.created,issue.updated,issue.status_changed" {
		t.Fatalf("types=%v", types)
	}
	if len(recorder.published) != 0 {
		t.Fatalf("unexpected persisted publications=%v", recorder.published)
	}
}

func TestUpdateIssueUsesLockedPreviousStatusAndPublishesAutoEnqueue(t *testing.T) {
	pid := coverageProjectID()
	base := &lockedStatusStore{
		fakeStore: fakeStore{project: store.Project{ID: pid, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}},
	}
	svc := New(base)
	recorder := &capturingIssueRecorder{}
	svc.SetEventRecorder(recorder)

	updated := coverageIssue()
	updated.Status = "IN_PROGRESS"
	got, err := svc.UpdateIssue(context.Background(), updated)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastEvent == nil || got.LastEvent.Type != "issue.status_changed" {
		t.Fatalf("lastEvent=%+v", got.LastEvent)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.LastEvent.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["previousStatus"] != "BLOCKED" {
		t.Fatalf("payload=%s", got.LastEvent.Payload)
	}
	if len(recorder.published) != 1 || recorder.published[0].ID != "run-event" || recorder.published[0].Type != "run.created" {
		t.Fatalf("published=%v", recorder.published)
	}
}

type lockedStatusStore struct {
	fakeStore
}

func (s *lockedStatusStore) GetIssue(context.Context, string, string) (store.Issue, error) {
	issue := coverageIssue()
	issue.Status = "BACKLOG"
	return issue, nil
}

func (s *lockedStatusStore) UpdateIssue(_ context.Context, input store.Issue) (store.Issue, error) {
	input.PreviousStatus = "BLOCKED"
	runEvent := store.Event{ID: "run-event", Type: "run.created", ProjectID: input.ProjectID, IssueID: &input.ID}
	input.LastEvent = &runEvent
	return input, nil
}

func TestIssueMutationsSkipEventsWhenRecorderMissing(t *testing.T) {
	pid := coverageProjectID()
	svc := New(&fakeStore{project: store.Project{ID: pid, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, agent: coverageAgent()})
	created, err := svc.CreateIssue(context.Background(), coverageIssue())
	if err != nil || created.LastEvent != nil {
		t.Fatalf("create without recorder=%+v err=%v", created, err)
	}
}
