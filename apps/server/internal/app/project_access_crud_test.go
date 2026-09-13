package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectAccessCRUDStore struct {
	store.ControlPlaneStore
	roles       map[string]string
	users       map[string]store.User
	groups      map[string]store.Group
	userGrants  map[string]store.ProjectUserAccess
	groupGrants map[string]store.ProjectGroupAccess
}

func newProjectAccessCRUDStore() *projectAccessCRUDStore {
	return &projectAccessCRUDStore{
		roles:       map[string]string{},
		users:       map[string]store.User{},
		groups:      map[string]store.Group{},
		userGrants:  map[string]store.ProjectUserAccess{},
		groupGrants: map[string]store.ProjectGroupAccess{},
	}
}

func accessCRUDKey(projectID, subjectID string) string { return projectID + ":" + subjectID }

func (s *projectAccessCRUDStore) CreateProjectWithAdmin(_ context.Context, input store.Project, userID string) (store.Project, error) {
	s.userGrants[accessCRUDKey(input.ID, userID)] = store.ProjectUserAccess{ProjectID: input.ID, UserID: userID, Role: store.ProjectRoleAdmin}
	return input, nil
}

func (s *projectAccessCRUDStore) ListProjectsForUser(context.Context, string) ([]store.Project, error) {
	return nil, nil
}

func (s *projectAccessCRUDStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	role, ok := s.roles[accessCRUDKey(projectID, userID)]
	if !ok {
		return "", store.ErrNotFound
	}
	return role, nil
}

func (s *projectAccessCRUDStore) ListProjectUserAccess(_ context.Context, projectID string) ([]store.ProjectUserAccessView, error) {
	values := make([]store.ProjectUserAccessView, 0)
	for _, grant := range s.userGrants {
		if grant.ProjectID == projectID {
			values = append(values, store.ProjectUserAccessView{Access: grant, User: s.users[grant.UserID]})
		}
	}
	return values, nil
}

func (s *projectAccessCRUDStore) UpsertProjectUserAccess(_ context.Context, input store.ProjectUserAccess) (store.ProjectUserAccess, error) {
	s.userGrants[accessCRUDKey(input.ProjectID, input.UserID)] = input
	return input, nil
}

func (s *projectAccessCRUDStore) DeleteProjectUserAccess(_ context.Context, projectID, userID string) error {
	key := accessCRUDKey(projectID, userID)
	if _, ok := s.userGrants[key]; !ok {
		return store.ErrNotFound
	}
	delete(s.userGrants, key)
	return nil
}

func (s *projectAccessCRUDStore) ListProjectGroupAccess(_ context.Context, projectID string) ([]store.ProjectGroupAccessView, error) {
	values := make([]store.ProjectGroupAccessView, 0)
	for _, grant := range s.groupGrants {
		if grant.ProjectID == projectID {
			values = append(values, store.ProjectGroupAccessView{Access: grant, Group: s.groups[grant.GroupID]})
		}
	}
	return values, nil
}

func (s *projectAccessCRUDStore) UpsertProjectGroupAccess(_ context.Context, input store.ProjectGroupAccess) (store.ProjectGroupAccess, error) {
	s.groupGrants[accessCRUDKey(input.ProjectID, input.GroupID)] = input
	return input, nil
}

func (s *projectAccessCRUDStore) DeleteProjectGroupAccess(_ context.Context, projectID, groupID string) error {
	key := accessCRUDKey(projectID, groupID)
	if _, ok := s.groupGrants[key]; !ok {
		return store.ErrNotFound
	}
	delete(s.groupGrants, key)
	return nil
}

func (s *projectAccessCRUDStore) GetProjectAccessUser(_ context.Context, userID string) (store.User, error) {
	user, ok := s.users[userID]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return user, nil
}

func (s *projectAccessCRUDStore) ListProjectAccessUsers(context.Context) ([]store.User, error) {
	values := make([]store.User, 0, len(s.users))
	for _, user := range s.users {
		values = append(values, user)
	}
	return values, nil
}

func (s *projectAccessCRUDStore) ListProjectAccessGroups(context.Context) ([]store.Group, error) {
	values := make([]store.Group, 0, len(s.groups))
	for _, group := range s.groups {
		values = append(values, group)
	}
	return values, nil
}

func newProjectAccessCRUDService(t *testing.T, fake *projectAccessCRUDStore) *ProjectAccessService {
	t.Helper()
	service, err := NewProjectAccessService(New(fake), fake)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestProjectAccessApplicationManagesDirectUserAndGroupGrants(t *testing.T) {
	const (
		projectID = "project-1"
		adminID   = "admin-1"
		userID    = "user-1"
		groupID   = "group-1"
	)
	fake := newProjectAccessCRUDStore()
	fake.roles[accessCRUDKey(projectID, adminID)] = store.ProjectRoleAdmin
	fake.users[userID] = store.User{ID: userID, Username: "alice", Status: store.UserStatusActive}
	fake.groups[groupID] = store.Group{ID: groupID, Name: "backend"}
	fake.userGrants[accessCRUDKey(projectID, userID)] = store.ProjectUserAccess{ProjectID: projectID, UserID: userID, Role: store.ProjectRoleViewer}
	fake.groupGrants[accessCRUDKey(projectID, groupID)] = store.ProjectGroupAccess{ProjectID: projectID, GroupID: groupID, Role: store.ProjectRoleViewer}
	service := newProjectAccessCRUDService(t, fake)
	actor := activeProjectActor(adminID, store.DeploymentRoleMember)

	users, err := service.ListUserAccess(t.Context(), actor, projectID)
	if err != nil || len(users) != 1 || users[0].User.ID != userID {
		t.Fatalf("ListUserAccess() = %+v, %v", users, err)
	}
	updatedUser, err := service.UpsertUserAccess(t.Context(), actor, store.ProjectUserAccess{ProjectID: projectID, UserID: userID, Role: store.ProjectRoleMember})
	if err != nil || updatedUser.Role != store.ProjectRoleMember {
		t.Fatalf("UpsertUserAccess() = %+v, %v", updatedUser, err)
	}
	if err := service.DeleteUserAccess(t.Context(), actor, projectID, userID); err != nil {
		t.Fatalf("DeleteUserAccess() = %v", err)
	}
	if _, ok := fake.userGrants[accessCRUDKey(projectID, userID)]; ok {
		t.Fatal("direct User grant was not deleted")
	}

	groups, err := service.ListGroupAccess(t.Context(), actor, projectID)
	if err != nil || len(groups) != 1 || groups[0].Group.ID != groupID {
		t.Fatalf("ListGroupAccess() = %+v, %v", groups, err)
	}
	updatedGroup, err := service.UpsertGroupAccess(t.Context(), actor, store.ProjectGroupAccess{ProjectID: projectID, GroupID: groupID, Role: store.ProjectRoleMember})
	if err != nil || updatedGroup.Role != store.ProjectRoleMember {
		t.Fatalf("UpsertGroupAccess() = %+v, %v", updatedGroup, err)
	}
	if err := service.DeleteGroupAccess(t.Context(), actor, projectID, groupID); err != nil {
		t.Fatalf("DeleteGroupAccess() = %v", err)
	}
	if _, ok := fake.groupGrants[accessCRUDKey(projectID, groupID)]; ok {
		t.Fatal("Group grant was not deleted")
	}
}

func TestProjectAccessApplicationValidatesRolesAndSubjects(t *testing.T) {
	const (
		projectID = "project-1"
		adminID   = "admin-1"
	)
	fake := newProjectAccessCRUDStore()
	fake.roles[accessCRUDKey(projectID, adminID)] = store.ProjectRoleAdmin
	service := newProjectAccessCRUDService(t, fake)
	actor := activeProjectActor(adminID, store.DeploymentRoleMember)

	if _, err := service.UpsertUserAccess(t.Context(), actor, store.ProjectUserAccess{ProjectID: projectID, UserID: "missing", Role: "owner"}); errorCode(err) != "invalid_argument" {
		t.Fatalf("invalid User role error = %v", err)
	}
	if _, err := service.UpsertUserAccess(t.Context(), actor, store.ProjectUserAccess{ProjectID: projectID, UserID: "missing", Role: store.ProjectRoleViewer}); errorCode(err) != "user_not_found" {
		t.Fatalf("missing User error = %v", err)
	}
	if _, err := service.UpsertGroupAccess(t.Context(), actor, store.ProjectGroupAccess{ProjectID: projectID, GroupID: "missing", Role: "owner"}); errorCode(err) != "invalid_argument" {
		t.Fatalf("invalid Group role error = %v", err)
	}
	if _, err := service.UpsertGroupAccess(t.Context(), actor, store.ProjectGroupAccess{ProjectID: projectID, GroupID: "missing", Role: store.ProjectRoleViewer}); errorCode(err) != "group_not_found" {
		t.Fatalf("missing Group error = %v", err)
	}
	if _, err := service.RequireRole(t.Context(), actor, projectID, "owner"); errorCode(err) != "invalid_argument" {
		t.Fatalf("invalid minimum role error = %v", err)
	}
}

func TestProjectAccessApplicationConstructorAndStoreErrorMappings(t *testing.T) {
	fake := newProjectAccessCRUDStore()
	if _, err := NewProjectAccessService(nil, fake); err == nil {
		t.Fatal("NewProjectAccessService(nil, store) unexpectedly succeeded")
	}
	if _, err := NewProjectAccessService(New(fake), nil); err == nil {
		t.Fatal("NewProjectAccessService(service, nil) unexpectedly succeeded")
	}

	cases := []struct {
		name string
		err  error
		code string
	}{
		{name: "nil", err: nil, code: ""},
		{name: "not found", err: store.ErrNotFound, code: "project_access_not_found"},
		{name: "invalid", err: store.ErrInvalidArgument, code: "invalid_argument"},
		{name: "conflict", err: store.ErrConflict, code: "conflict"},
		{name: "last admin", err: store.ErrLastProjectAdmin, code: "last_project_admin"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := projectAccessStoreError(tt.err)
			if tt.code == "" {
				if got != nil {
					t.Fatalf("projectAccessStoreError(nil) = %v", got)
				}
				return
			}
			if errorCode(got) != tt.code {
				t.Fatalf("projectAccessStoreError(%v) = %v", tt.err, got)
			}
		})
	}

	opaque := errors.New("database unavailable")
	if got := projectAccessStoreError(opaque); !errors.Is(got, opaque) {
		t.Fatalf("opaque store error = %v", got)
	}
}

func errorCode(err error) string {
	if apiErr, ok := AsError(err); ok {
		return apiErr.Code
	}
	return ""
}
