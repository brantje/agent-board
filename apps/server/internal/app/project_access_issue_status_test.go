package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *projectWorkflowAuthorizationStore) UpdateIssuePatchMutation(_ context.Context, patch store.IssuePatch, actor json.RawMessage) (store.IssueMutationResult, error) {
	previous, err := s.GetIssue(context.Background(), patch.ProjectID, patch.ID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	updated := previous
	if patch.Title != nil {
		updated.Title = *patch.Title
	}
	if patch.Description != nil {
		updated.Description = *patch.Description
	}
	if patch.Status != nil {
		updated.Status = *patch.Status
	}
	if patch.Priority != nil {
		updated.Priority = *patch.Priority
	}
	if updated.Title == previous.Title && updated.Description == previous.Description && updated.Status == previous.Status && updated.Priority == previous.Priority {
		return store.IssueMutationResult{Issue: previous}, nil
	}
	event, err := store.NewIssueUpdatedEventWithActor(updated, previous.Status, actor)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	s.updateIssueCalls++
	updated.LastEvent = &event
	s.issues[updated.ID] = updated
	return store.IssueMutationResult{Issue: updated, Events: []store.Event{event}}, nil
}

func TestProjectAccessIssueStatusUsesAuthenticatedHumanActor(t *testing.T) {
	const projectID = "project-1"
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB"},
		roles: map[string]string{
			"member-user": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{
			"issue-existing": {ID: "issue-existing", ProjectID: projectID, Number: 1, Title: "Existing", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	access := newProjectWorkflowAccess(t, fake)
	actor := activeProjectActor("member-user", store.DeploymentRoleMember)
	input := fake.issues["issue-existing"]
	input.Status = "IN_PROGRESS"

	updated, err := access.UpdateIssue(t.Context(), actor, input)
	if err != nil {
		t.Fatalf("UpdateIssue() error=%v", err)
	}
	if updated.Status != "IN_PROGRESS" || fake.updateIssueCalls != 1 {
		t.Fatalf("updated=%+v mutationCalls=%d", updated, fake.updateIssueCalls)
	}
	assertHumanIssueEventActor(t, updated.LastEvent, actor.ID, "issue.status_changed")
}

func TestProjectAccessCombinedIssueEditUsesOneActorAttributedMutation(t *testing.T) {
	const projectID = "project-1"
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB"},
		roles: map[string]string{
			"member-user": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{
			"issue-existing": {ID: "issue-existing", ProjectID: projectID, Number: 1, Title: "Existing", Status: "TODO", Priority: 1},
		},
		runs: map[string]store.Run{},
	}
	access := newProjectWorkflowAccess(t, fake)
	actor := activeProjectActor("member-user", store.DeploymentRoleMember)
	input := fake.issues["issue-existing"]
	input.Status = "REVIEW"
	input.Title = "Updated title"
	input.Description = "Updated description"
	input.Priority = 3

	updated, err := access.UpdateIssue(t.Context(), actor, input)
	if err != nil {
		t.Fatalf("UpdateIssue() error=%v", err)
	}
	if fake.updateIssueCalls != 1 {
		t.Fatalf("mutationCalls=%d want=1", fake.updateIssueCalls)
	}
	if updated.Status != input.Status || updated.Title != input.Title || updated.Description != input.Description || updated.Priority != input.Priority {
		t.Fatalf("updated=%+v want=%+v", updated, input)
	}
	assertHumanIssueEventActor(t, updated.LastEvent, actor.ID, "issue.status_changed")
}

func TestProjectAccessIssueUpdateNoOpDoesNotWrite(t *testing.T) {
	const projectID = "project-1"
	fake := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB"},
		roles: map[string]string{
			"member-user": store.ProjectRoleMember,
		},
		issues: map[string]store.Issue{
			"issue-existing": {ID: "issue-existing", ProjectID: projectID, Number: 1, Title: "Existing", Status: "TODO", Priority: 2},
		},
		runs: map[string]store.Run{},
	}
	access := newProjectWorkflowAccess(t, fake)
	actor := activeProjectActor("member-user", store.DeploymentRoleMember)
	input := fake.issues["issue-existing"]

	updated, err := access.UpdateIssue(t.Context(), actor, input)
	if err != nil {
		t.Fatalf("UpdateIssue() error=%v", err)
	}
	if updated.Status != input.Status || fake.updateIssueCalls != 0 {
		t.Fatalf("updated=%+v mutationCalls=%d", updated, fake.updateIssueCalls)
	}
}

func assertHumanIssueEventActor(t *testing.T, event *store.Event, actorID, eventType string) {
	t.Helper()
	if event == nil || event.Type != eventType {
		t.Fatalf("last event=%+v want type=%s", event, eventType)
	}
	var attributed struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(event.Actor, &attributed); err != nil {
		t.Fatalf("decode actor: %v", err)
	}
	if attributed.Type != store.ActorTypeHuman || attributed.ID != actorID {
		t.Fatalf("actor=%+v", attributed)
	}
}
