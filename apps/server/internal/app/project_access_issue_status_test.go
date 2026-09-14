package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *projectWorkflowAuthorizationStore) SetIssueStatus(_ context.Context, mutation store.IssueStatusMutation) (store.IssueMutationResult, error) {
	issue, err := s.GetIssue(context.Background(), mutation.ProjectID, mutation.IssueID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if issue.Status == mutation.Status {
		return store.IssueMutationResult{Issue: issue}, nil
	}
	previousStatus := issue.Status
	issue.Status = mutation.Status
	payload, _ := json.Marshal(map[string]any{"status": issue.Status, "previousStatus": previousStatus})
	event := store.Event{
		Type:      "issue.status_changed",
		ProjectID: mutation.ProjectID,
		IssueID:   &issue.ID,
		Actor:     append(json.RawMessage(nil), mutation.Actor...),
		Payload:   payload,
	}
	issue.LastEvent = &event
	s.issues[issue.ID] = issue
	return store.IssueMutationResult{Issue: issue, Events: []store.Event{event}}, nil
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
	if updated.Status != "IN_PROGRESS" || fake.updateIssueCalls != 0 {
		t.Fatalf("updated=%+v genericUpdateCalls=%d", updated, fake.updateIssueCalls)
	}
	if updated.LastEvent == nil || updated.LastEvent.Type != "issue.status_changed" {
		t.Fatalf("last event=%+v", updated.LastEvent)
	}
	var attributed struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(updated.LastEvent.Actor, &attributed); err != nil {
		t.Fatalf("decode actor: %v", err)
	}
	if attributed.Type != store.ActorTypeHuman || attributed.ID != actor.ID {
		t.Fatalf("actor=%+v", attributed)
	}
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
		t.Fatalf("updated=%+v genericUpdateCalls=%d", updated, fake.updateIssueCalls)
	}
}
