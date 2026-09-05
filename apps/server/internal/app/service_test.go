package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type fakeStore struct {
	store.ControlPlaneStore
	project store.Project
	agent   store.Agent
}

func (f *fakeStore) GetProject(context.Context, string) (store.Project, error) {
	if f.project.ID == "" {
		return store.Project{}, store.ErrNotFound
	}
	return f.project, nil
}
func (f *fakeStore) GetAgentInScope(context.Context, *string, string) (store.Agent, error) {
	if f.agent.ID == "" {
		return store.Agent{}, store.ErrNotFound
	}
	return f.agent, nil
}

func TestCreateRuntimeRejectsInvalidPolicyBeforeStore(t *testing.T) {
	projectID := "project"
	svc := New(&fakeStore{project: store.Project{ID: projectID}})
	_, err := svc.CreateRuntime(context.Background(), store.Runtime{ProjectID: &projectID, Name: "runtime", Kind: "docker", Image: "image", NetworkPolicy: "internet", WorkspacePolicy: "issue", Enabled: true})
	appErr, ok := AsError(err)
	if !ok || appErr.Code != "invalid_argument" {
		t.Fatalf("error = %#v", err)
	}
}

func TestGetAgentHidesMissingScopedAgent(t *testing.T) {
	projectID := "project"
	svc := New(&fakeStore{project: store.Project{ID: projectID}})
	_, err := svc.GetAgent(context.Background(), &projectID, "other-project-agent")
	appErr, ok := AsError(err)
	if !ok || appErr.Code != "agent_not_found" || !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %#v", err)
	}
}
