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
	createdIssue  store.Issue
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

func (s *projectAccessServiceStore) CreateIssue(_ context.Context, input store.Issue) (store.Issue, error) {
	s.createdIssue = input
	input.ID = "issue-1"
	input.Key = "AB-1"
	return input, nil
}

func (s *projectAccessServiceStore) CreateIssueMutation(ctx context.Context, input store.Issue) (store.IssueMutationResult, error) {
	issue, err := s.CreateIssue(ctx, input)
	return store.IssueMutationResult{Issue: issue}, err
}

func (s *projectAccessServiceStore) UpdateIssueMutation(_ context.Context, input store.Issue) (store.IssueMutationResult, error) {
	return store.IssueMutationResult{Issue: input}, nil
}

func (s *projectAccessServiceStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	for _, user := range s.users {
		if user.ID == userID && user.DeploymentRole == store.DeploymentRoleAdmin {
			return store.ProjectRoleAdmin, nil
		}
	}
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

func TestProjectAccessCreateIssueAttributesAuthenticatedHuman(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	fake := &projectAccessServiceStore{
		projects: []store.Project{project},
		roles:    map[string]string{project.ID + ":user-1": store.ProjectRoleMember},
	}
	service := newProjectAccessServiceForTest(t, fake)
	actor := activeProjectActor("user-1", store.DeploymentRoleMember)

	created, err := service.CreateIssue(t.Context(), actor, store.Issue{ProjectID: project.ID, Title: "Attributed", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	for name, issue := range map[string]store.Issue{"input": fake.createdIssue, "result": created} {
		if issue.CreatedByType == nil || *issue.CreatedByType != store.ActorTypeHuman || issue.CreatedByID == nil || *issue.CreatedByID != actor.ID {
			t.Fatalf("%s creator = type=%v id=%v", name, issue.CreatedByType, issue.CreatedByID)
		}
	}
}

func TestProjectAccessDeploymentAdminHasImplicitAdminRole(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	fake := &projectAccessServiceStore{
		projects: []store.Project{project},
		users: []store.User{{ID: "deployment-admin", DeploymentRole: store.DeploymentRoleAdmin, Status: store.UserStatusActive}},
	}
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
			{ID: "u2", Username: "bob", Email: "bob@example.com", DisplayName: "Bob", Status: store.UserStatusDisabled},
		},
		groups: []store.Group{{ID: "g1", Name: "Core"}},
	}
	service := newProjectAccessServiceForTest(t, fake)
	admin := activeProjectActor("admin", store.DeploymentRoleMember)
	member := activeProjectActor("member", store.DeploymentRoleMember)

	users, err := service.SearchUsers(t.Context(), admin, project.ID, "ali")
	if err != nil || len(users) != 1 || users[0].ID != "u1" {
		t.Fatalf("users=%+v err=%v", users, err)
	}
	groups, err := service.SearchGroups(t.Context(), admin, project.ID, "cor")
	if err != nil || len(groups) != 1 || groups[0].ID != "g1" {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	if _, err := service.SearchUsers(t.Context(), member, project.ID, ""); err == nil {
		t.Fatal("member unexpectedly searched project users")
	}
}

func TestProjectAccessConstructionAndInvalidRoleErrors(t *testing.T) {
	if _, err := NewProjectAccessService(nil, &projectAccessServiceStore{}); err == nil {
		t.Fatal("expected nil control plane rejection")
	}
	if _, err := NewProjectAccessService(New(&projectAccessServiceStore{}), nil); err == nil {
		t.Fatal("expected nil access store rejection")
	}

	service := newProjectAccessServiceForTest(t, &projectAccessServiceStore{})
	if _, err := service.RequireRole(t.Context(), activeProjectActor("u", store.DeploymentRoleMember), "p", "owner"); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("invalid role error=%v", err)
	}
}
