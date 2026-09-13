package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectAccessServiceStore struct {
	store.ControlPlaneStore
	store.ProjectAccessStore
	projects      []store.Project
	visible       []store.Project
	roles         map[string]string
	users         []store.User
	groups        []store.Group
	listAllCalls  int
	listUserCalls int
}

func (s *projectAccessServiceStore) ListProjects(context.Context) ([]store.Project, error) {
	s.listAllCalls++
	return append([]store.Project(nil), s.projects...), nil
}

func (s *projectAccessServiceStore) GetProject(_ context.Context, id string) (store.Project, error) {
	for _, project := range s.projects {
		if project.ID == id {
			return project, nil
		}
	}
	return store.Project{}, store.ErrNotFound
}

func (s *projectAccessServiceStore) ListProjectsForUser(context.Context, string) ([]store.Project, error) {
	s.listUserCalls++
	return append([]store.Project(nil), s.visible...), nil
}

func (s *projectAccessServiceStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	role, ok := s.roles[projectID+":"+userID]
	if !ok {
		return "", store.ErrNotFound
	}
	return role, nil
}

func (s *projectAccessServiceStore) ListProjectAccessUsers(context.Context) ([]store.User, error) {
	return append([]store.User(nil), s.users...), nil
}

func (s *projectAccessServiceStore) ListProjectAccessGroups(context.Context) ([]store.Group, error) {
	return append([]store.Group(nil), s.groups...), nil
}

func newProjectAccessServiceForTest(t *testing.T, fake *projectAccessServiceStore) *ProjectAccessService {
	t.Helper()
	service, err := NewProjectAccessService(New(fake), fake)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func activeProjectActor(id, deploymentRole string) AuthenticatedUser {
	return AuthenticatedUser{ID: id, DeploymentRole: deploymentRole, Status: store.UserStatusActive}
}

func TestProjectAccessListsOnlyAccessibleProjectsForMembers(t *testing.T) {
	project := store.Project{ID: "project-visible", Name: "Visible"}
	fake := &projectAccessServiceStore{
		projects: []store.Project{project, {ID: "project-hidden", Name: "Hidden"}},
		visible:  []store.Project{project},
	}
	service := newProjectAccessServiceForTest(t, fake)

	projects, err := service.ListProjects(t.Context(), activeProjectActor("user-1", store.DeploymentRoleMember))
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("projects = %+v", projects)
	}
	if fake.listUserCalls != 1 || fake.listAllCalls != 0 {
		t.Fatalf("list calls: scoped=%d all=%d", fake.listUserCalls, fake.listAllCalls)
	}
}

func TestProjectAccessDeploymentAdminHasImplicitAdminRole(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	fake := &projectAccessServiceStore{projects: []store.Project{project}}
	service := newProjectAccessServiceForTest(t, fake)
	actor := activeProjectActor("deployment-admin", store.DeploymentRoleAdmin)

	role, err := service.RequireRole(t.Context(), actor, project.ID, store.ProjectRoleAdmin)
	if err != nil || role != store.ProjectRoleAdmin {
		t.Fatalf("role=%q err=%v", role, err)
	}
	projects, err := service.ListProjects(t.Context(), actor)
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects=%+v err=%v", projects, err)
	}
	if fake.listAllCalls != 1 || fake.listUserCalls != 0 {
		t.Fatalf("list calls: scoped=%d all=%d", fake.listUserCalls, fake.listAllCalls)
	}
}

func TestProjectAccessRoleChecksPreserveNotFoundIsolation(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	fake := &projectAccessServiceStore{
		projects: []store.Project{project},
		roles:    map[string]string{project.ID + ":viewer": store.ProjectRoleViewer},
	}
	service := newProjectAccessServiceForTest(t, fake)

	if _, err := service.RequireRole(t.Context(), activeProjectActor("viewer", store.DeploymentRoleMember), project.ID, store.ProjectRoleMember); err == nil {
		t.Fatal("viewer unexpectedly received member authorization")
	} else if apiErr, ok := AsError(err); !ok || apiErr.Code != "forbidden" {
		t.Fatalf("viewer denial = %v", err)
	}

	_, _, err := service.GetProject(t.Context(), activeProjectActor("unrelated", store.DeploymentRoleMember), project.ID)
	if err == nil {
		t.Fatal("inaccessible project unexpectedly returned")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "project_not_found" {
		t.Fatalf("inaccessible project error = %v", err)
	}
}

func TestProjectAccessDirectoryIsAdminOnlyAndReturnsSafeActiveIdentity(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	fake := &projectAccessServiceStore{
		projects: []store.Project{project},
		roles: map[string]string{
			project.ID + ":admin":  store.ProjectRoleAdmin,
			project.ID + ":member": store.ProjectRoleMember,
		},
		users: []store.User{
			{ID: "u1", Username: "alice", Email: "alice@example.com", DisplayName: "Alice", Status: store.UserStatusActive, PasswordHash: "secret-hash", AuthVersion: 7},
			{ID: "u2", Username: "disabled", Email: "disabled@example.com", DisplayName: "Disabled", Status: store.UserStatusDisabled},
		},
		groups: []store.Group{{ID: "g1", Name: "backend"}, {ID: "g2", Name: "frontend"}},
	}
	service := newProjectAccessServiceForTest(t, fake)

	if _, err := service.SearchUsers(t.Context(), activeProjectActor("member", store.DeploymentRoleMember), project.ID, "alice"); err == nil {
		t.Fatal("project member unexpectedly searched directory")
	}
	users, err := service.SearchUsers(t.Context(), activeProjectActor("admin", store.DeploymentRoleMember), project.ID, "ali")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0] != (ProjectDirectoryUser{ID: "u1", Username: "alice", Email: "alice@example.com", DisplayName: "Alice"}) {
		t.Fatalf("safe users = %+v", users)
	}
	groups, err := service.SearchGroups(t.Context(), activeProjectActor("admin", store.DeploymentRoleMember), project.ID, "back")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0] != (ProjectDirectoryGroup{ID: "g1", Name: "backend"}) {
		t.Fatalf("safe groups = %+v", groups)
	}
}

func TestProjectAccessStoreErrorsMapLastDirectAdmin(t *testing.T) {
	err := projectAccessStoreError(store.ErrLastProjectAdmin)
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "last_project_admin" || !errors.Is(apiErr.Err, store.ErrLastProjectAdmin) {
		t.Fatalf("mapped error = %#v", err)
	}
}
