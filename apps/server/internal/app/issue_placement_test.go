package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type recordingIssuePlacementStore struct {
	*fakeStore
	placement store.IssuePlacement
	actor     json.RawMessage
	result    store.IssueMutationResult
	err       error
	calls     int
}

func (s *recordingIssuePlacementStore) PlaceIssue(_ context.Context, placement store.IssuePlacement, actor json.RawMessage) (store.IssueMutationResult, error) {
	s.calls++
	s.placement = placement
	s.actor = append(json.RawMessage(nil), actor...)
	if s.err != nil {
		return store.IssueMutationResult{}, s.err
	}
	return s.result, nil
}

func TestIssuePlacementApplicationBoundaryDelegatesActorAndPublishesEvents(t *testing.T) {
	project := store.Project{ID: "project-1"}
	event := store.Event{ID: "event-1", Type: "issue.status_changed", ProjectID: project.ID}
	recording := &recordingIssuePlacementStore{
		fakeStore: &fakeStore{project: project},
		result: store.IssueMutationResult{
			Issue:  store.Issue{ID: "issue-1", ProjectID: project.ID, Status: "REVIEW"},
			Events: []store.Event{event},
		},
	}
	service := New(recording)
	publisher := &assigneePublisher{}
	service.SetEventRecorder(publisher)
	status := "REVIEW"
	beforeID := "issue-2"
	input := store.IssuePlacement{ProjectID: project.ID, IssueID: "issue-1", Status: &status, BeforeID: &beforeID}
	actor := json.RawMessage(`{"type":"HUMAN","id":"user-1"}`)

	issue, err := service.PlaceIssueWithActor(t.Context(), input, actor)
	if err != nil {
		t.Fatal(err)
	}
	if issue.ID != "issue-1" || issue.Status != status {
		t.Fatalf("issue=%+v", issue)
	}
	if recording.calls != 1 || recording.placement.IssueID != input.IssueID || recording.placement.BeforeID == nil || *recording.placement.BeforeID != beforeID {
		t.Fatalf("calls=%d placement=%+v", recording.calls, recording.placement)
	}
	if string(recording.actor) != string(actor) {
		t.Fatalf("actor=%s want=%s", recording.actor, actor)
	}
	if len(publisher.published) != 1 || publisher.published[0].ID != event.ID {
		t.Fatalf("published=%+v", publisher.published)
	}

	recording.actor = nil
	if _, err := service.PlaceIssue(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if string(recording.actor) != string(store.EmptyObject) {
		t.Fatalf("default actor=%s want=%s", recording.actor, store.EmptyObject)
	}
}

func TestIssuePlacementApplicationBoundaryValidatesCapabilityAndStoreErrors(t *testing.T) {
	project := store.Project{ID: "project-1"}
	valid := store.IssuePlacement{ProjectID: project.ID, IssueID: "issue-1"}
	service := New(&recordingIssuePlacementStore{fakeStore: &fakeStore{project: project}})
	badStatus := "NOPE"
	self := valid.IssueID
	other := "issue-2"

	for name, input := range map[string]store.IssuePlacement{
		"missing project": {IssueID: valid.IssueID},
		"missing issue":   {ProjectID: project.ID},
		"invalid status":  {ProjectID: project.ID, IssueID: valid.IssueID, Status: &badStatus},
		"before self":     {ProjectID: project.ID, IssueID: valid.IssueID, BeforeID: &self},
		"after self":      {ProjectID: project.ID, IssueID: valid.IssueID, AfterID: &self},
		"same anchors":    {ProjectID: project.ID, IssueID: valid.IssueID, BeforeID: &other, AfterID: &other},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.PlaceIssue(t.Context(), input); err == nil {
				t.Fatal("invalid placement unexpectedly succeeded")
			} else if appErr, ok := AsError(err); !ok || appErr.Code != "invalid_argument" {
				t.Fatalf("error=%v", err)
			}
		})
	}

	withoutCapability := New(&fakeStore{project: project})
	if _, err := withoutCapability.PlaceIssue(t.Context(), valid); err == nil {
		t.Fatal("missing placement capability unexpectedly succeeded")
	} else if appErr, ok := AsError(err); !ok || appErr.Code != "issue_placement_unavailable" {
		t.Fatalf("missing capability error=%v", err)
	}

	failing := &recordingIssuePlacementStore{fakeStore: &fakeStore{project: project}, err: store.ErrConflict}
	if _, err := New(failing).PlaceIssue(t.Context(), valid); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("translated error=%v", err)
	}

	if _, err := service.PlaceIssue(t.Context(), store.IssuePlacement{ProjectID: "missing", IssueID: valid.IssueID}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("project lookup error=%v", err)
	}
}

type projectAccessPlacementStore struct {
	*projectAccessServiceStore
	placement store.IssuePlacement
	actor     json.RawMessage
	calls     int
}

func (s *projectAccessPlacementStore) PlaceIssue(_ context.Context, placement store.IssuePlacement, actor json.RawMessage) (store.IssueMutationResult, error) {
	s.calls++
	s.placement = placement
	s.actor = append(json.RawMessage(nil), actor...)
	return store.IssueMutationResult{Issue: store.Issue{ID: placement.IssueID, ProjectID: placement.ProjectID, Status: "TODO"}}, nil
}

func TestProjectAccessPlaceIssueAuthorizesAndAttributesHumanActor(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	base := &projectAccessServiceStore{
		projects: []store.Project{project},
		roles: map[string]string{
			project.ID + ":member": store.ProjectRoleMember,
			project.ID + ":viewer": store.ProjectRoleViewer,
		},
	}
	recording := &projectAccessPlacementStore{projectAccessServiceStore: base}
	controlPlane := New(recording)
	service, err := NewProjectAccessService(controlPlane, recording)
	if err != nil {
		t.Fatal(err)
	}
	input := store.IssuePlacement{ProjectID: project.ID, IssueID: "issue-1"}

	if _, err := service.PlaceIssue(t.Context(), activeProjectActor("viewer", store.DeploymentRoleMember), input); err == nil {
		t.Fatal("viewer unexpectedly moved issue")
	}
	if recording.calls != 0 {
		t.Fatalf("unauthorized placement reached store: calls=%d", recording.calls)
	}

	issue, err := service.PlaceIssue(t.Context(), activeProjectActor("member", store.DeploymentRoleMember), input)
	if err != nil {
		t.Fatal(err)
	}
	if issue.ID != input.IssueID || recording.calls != 1 || recording.placement.IssueID != input.IssueID {
		t.Fatalf("issue=%+v calls=%d placement=%+v", issue, recording.calls, recording.placement)
	}
	if string(recording.actor) != `{"id":"member","type":"HUMAN"}` {
		t.Fatalf("actor=%s", recording.actor)
	}
}
