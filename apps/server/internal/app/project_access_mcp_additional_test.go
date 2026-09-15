package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *projectAccessServiceStore) GetIssue(ctx context.Context, projectID, issueID string) (store.Issue, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return store.Issue{}, err
	}
	if issueID == "" {
		return store.Issue{}, store.ErrNotFound
	}
	return store.Issue{ID: issueID, ProjectID: projectID, Title: "Issue", Status: "TODO"}, nil
}

func (s *projectAccessServiceStore) ListAgents(_ context.Context, projectID *string) ([]store.Agent, error) {
	if projectID == nil {
		return nil, store.ErrNotFound
	}
	return []store.Agent{{ID: "agent-1", ProjectID: projectID, Name: "Agent"}}, nil
}

func (s *projectAccessServiceStore) GetAgentInScope(_ context.Context, projectID *string, agentID string) (store.Agent, error) {
	if projectID == nil || agentID != "agent-1" {
		return store.Agent{}, store.ErrNotFound
	}
	return store.Agent{ID: agentID, ProjectID: projectID, Name: "Agent"}, nil
}

func (s *projectAccessServiceStore) ListIssueRelationships(_ context.Context, projectID, sourceIssueID string) ([]store.IssueRelationship, error) {
	return []store.IssueRelationship{{ID: "relationship-1", ProjectID: projectID, SourceIssueID: sourceIssueID, TargetIssueID: "issue-2", Type: "blocks"}}, nil
}

func (s *projectAccessServiceStore) CreateIssueRelationship(_ context.Context, input store.IssueRelationship) (store.IssueRelationship, error) {
	input.ID = "relationship-1"
	return input, nil
}

func (s *projectAccessServiceStore) DeleteIssueRelationship(context.Context, string, string, string) error {
	return nil
}

func (s *projectAccessServiceStore) GetIssueExecutionState(context.Context, string, string) (store.IssueExecutionState, error) {
	return store.IssueExecutionState{State: store.IssueExecutionReady, CanStart: true}, nil
}

func TestProjectAccessMCPDelegatesAuthorizedOperations(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	fake := &projectAccessServiceStore{
		projects: []store.Project{project},
		roles:    map[string]string{project.ID + ":member": store.ProjectRoleMember},
	}
	service := newProjectAccessServiceForTest(t, fake)
	actor := activeProjectActor("member", store.DeploymentRoleMember)

	agents, err := service.ListAgents(t.Context(), actor, project.ID)
	if err != nil || len(agents) != 1 || agents[0].ID != "agent-1" {
		t.Fatalf("agents=%+v err=%v", agents, err)
	}
	agent, err := service.GetAgent(t.Context(), actor, project.ID, "agent-1")
	if err != nil || agent.ID != "agent-1" {
		t.Fatalf("agent=%+v err=%v", agent, err)
	}

	relationships, err := service.ListIssueRelationships(t.Context(), actor, project.ID, "issue-1")
	if err != nil || len(relationships) != 1 || relationships[0].ID != "relationship-1" {
		t.Fatalf("relationships=%+v err=%v", relationships, err)
	}
	created, err := service.CreateIssueRelationship(t.Context(), actor, store.IssueRelationship{
		ProjectID: project.ID, SourceIssueID: "issue-1", TargetIssueID: "issue-2", Type: "blocks",
	})
	if err != nil || created.ID != "relationship-1" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	if err := service.DeleteIssueRelationship(t.Context(), actor, project.ID, "issue-1", "relationship-1"); err != nil {
		t.Fatal(err)
	}

	state, err := service.GetIssueExecutionState(t.Context(), actor, project.ID, "issue-1")
	if err != nil || state.State != store.IssueExecutionReady || !state.CanStart {
		t.Fatalf("state=%+v err=%v", state, err)
	}

	if _, err := service.InspectRun(t.Context(), actor, nil, project.ID, "run-1"); err == nil {
		t.Fatal("nil run evidence service unexpectedly accepted")
	}
	if _, _, err := service.OpenRunRawOutput(t.Context(), actor, nil, project.ID, "run-1", "chunk-1"); err == nil {
		t.Fatal("nil run evidence service unexpectedly accepted")
	}
}
