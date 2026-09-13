package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectAccessProjectStore struct {
	store.ControlPlaneStore
	store.ProjectAccessStore
	projects      map[string]store.Project
	roles         map[string]string
	createdBy     string
	createCalls   int
	updateCalls   int
}

func newProjectAccessProjectStore() *projectAccessProjectStore {
	return &projectAccessProjectStore{
		projects: map[string]store.Project{},
		roles:    map[string]string{},
	}
}

func projectAccessProjectKey(projectID, userID string) string { return projectID + ":" + userID }

func (s *projectAccessProjectStore) CreateProjectWithAdmin(_ context.Context, input store.Project, userID string) (store.Project, error) {
	s.createCalls++
	s.createdBy = userID
	if input.ID == "" {
		input.ID = "project-created"
	}
	s.projects[input.ID] = input
	s.roles[projectAccessProjectKey(input.ID, userID)] = store.ProjectRoleAdmin
	return input, nil
}

func (s *projectAccessProjectStore) ListProjectsForUser(_ context.Context, userID string) ([]store.Project, error) {
	values := make([]store.Project, 0)
	for id, project := range s.projects {
		if _, ok := s.roles[projectAccessProjectKey(id, userID)]; ok {
			values = append(values, project)
		}
	}
	return values, nil
}

func (s *projectAccessProjectStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	role, ok := s.roles[projectAccessProjectKey(projectID, userID)]
	if !ok {
		return "", store.ErrNotFound
	}
	return role, nil
}

func (s *projectAccessProjectStore) GetProject(_ context.Context, id string) (store.Project, error) {
	project, ok := s.projects[id]
	if !ok {
		return store.Project{}, store.ErrNotFound
	}
	return project, nil
}

func (s *projectAccessProjectStore) UpdateProject(_ context.Context, input store.Project) (store.Project, error) {
	if _, ok := s.projects[input.ID]; !ok {
		return store.Project{}, store.ErrNotFound
	}
	s.updateCalls++
	s.projects[input.ID] = input
	return input, nil
}

func projectAccessValidProject(id, name string) store.Project {
	return store.Project{
		ID:               id,
		Name:             name,
		IssuePrefix:      "PAC",
		SourceType:       store.ProjectSourceLocal,
		RepositoryPath:   "/repo/project-access",
		WorkflowSettings: store.EmptyObject,
	}
}

func TestProjectAccessApplicationCreatesPrivateProjectWithCreatorAdmin(t *testing.T) {
	fake := newProjectAccessProjectStore()
	service, err := NewProjectAccessService(New(fake), fake)
	if err != nil {
		t.Fatal(err)
	}
	actor := activeProjectActor("creator-1", store.DeploymentRoleMember)

	created, err := service.CreateProject(t.Context(), actor, projectAccessValidProject("", "Created"))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "project-created" || created.DefaultBranch != "main" {
		t.Fatalf("created project = %+v", created)
	}
	if fake.createdBy != actor.ID || fake.createCalls != 1 {
		t.Fatalf("creator=%q calls=%d", fake.createdBy, fake.createCalls)
	}
	role, err := service.EffectiveRole(t.Context(), actor, created.ID)
	if err != nil || role != store.ProjectRoleAdmin {
		t.Fatalf("creator role = %q, %v", role, err)
	}
}

func TestProjectAccessApplicationProjectCreationValidatesActorAndInput(t *testing.T) {
	fake := newProjectAccessProjectStore()
	service, err := NewProjectAccessService(New(fake), fake)
	if err != nil {
		t.Fatal(err)
	}
	input := projectAccessValidProject("", "Created")

	unknownRole := AuthenticatedUser{ID: "user-1", DeploymentRole: "operator", Status: store.UserStatusActive}
	if _, err := service.CreateProject(t.Context(), unknownRole, input); errorCode(err) != "forbidden" {
		t.Fatalf("unknown deployment role create error = %v", err)
	}
	invalid := input
	invalid.Name = ""
	if _, err := service.CreateProject(t.Context(), activeProjectActor("member-1", store.DeploymentRoleMember), invalid); errorCode(err) != "invalid_argument" {
		t.Fatalf("invalid project create error = %v", err)
	}
	if fake.createCalls != 0 {
		t.Fatalf("store create calls = %d", fake.createCalls)
	}
}

func TestProjectAccessApplicationUpdatesProjectOnlyForProjectAdmin(t *testing.T) {
	const projectID = "project-existing"
	fake := newProjectAccessProjectStore()
	fake.projects[projectID] = projectAccessValidProject(projectID, "Before")
	fake.roles[projectAccessProjectKey(projectID, "admin-1")] = store.ProjectRoleAdmin
	fake.roles[projectAccessProjectKey(projectID, "member-1")] = store.ProjectRoleMember
	service, err := NewProjectAccessService(New(fake), fake)
	if err != nil {
		t.Fatal(err)
	}

	input := projectAccessValidProject(projectID, "After")
	if _, err := service.UpdateProject(t.Context(), activeProjectActor("member-1", store.DeploymentRoleMember), input); errorCode(err) != "forbidden" {
		t.Fatalf("member update error = %v", err)
	}
	updated, err := service.UpdateProject(t.Context(), activeProjectActor("admin-1", store.DeploymentRoleMember), input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "After" || updated.DefaultBranch != "main" || fake.updateCalls != 1 {
		t.Fatalf("updated project = %+v calls=%d", updated, fake.updateCalls)
	}

	project, role, err := service.GetProject(t.Context(), activeProjectActor("admin-1", store.DeploymentRoleMember), projectID)
	if err != nil || role != store.ProjectRoleAdmin || project.Name != "After" {
		t.Fatalf("GetProject() = %+v role=%q err=%v", project, role, err)
	}
}
