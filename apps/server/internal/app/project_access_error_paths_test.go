package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectAccessFailureStore struct {
	*projectAccessCRUDStore
	effectiveErr   error
	getUserErr     error
	listUsersErr   error
	listGroupsErr  error
	userGrantsErr  error
	groupGrantsErr error
	deleteUserErr  error
	deleteGroupErr error
}

func (s *projectAccessFailureStore) EffectiveProjectRole(ctx context.Context, projectID, userID string) (string, error) {
	if s.effectiveErr != nil {
		return "", s.effectiveErr
	}
	return s.projectAccessCRUDStore.EffectiveProjectRole(ctx, projectID, userID)
}

func (s *projectAccessFailureStore) GetProjectAccessUser(ctx context.Context, userID string) (store.User, error) {
	if s.getUserErr != nil {
		return store.User{}, s.getUserErr
	}
	return s.projectAccessCRUDStore.GetProjectAccessUser(ctx, userID)
}

func (s *projectAccessFailureStore) ListProjectAccessUsers(ctx context.Context) ([]store.User, error) {
	if s.listUsersErr != nil {
		return nil, s.listUsersErr
	}
	return s.projectAccessCRUDStore.ListProjectAccessUsers(ctx)
}

func (s *projectAccessFailureStore) ListProjectAccessGroups(ctx context.Context) ([]store.Group, error) {
	if s.listGroupsErr != nil {
		return nil, s.listGroupsErr
	}
	return s.projectAccessCRUDStore.ListProjectAccessGroups(ctx)
}

func (s *projectAccessFailureStore) ListProjectUserAccess(ctx context.Context, projectID string) ([]store.ProjectUserAccessView, error) {
	if s.userGrantsErr != nil {
		return nil, s.userGrantsErr
	}
	return s.projectAccessCRUDStore.ListProjectUserAccess(ctx, projectID)
}

func (s *projectAccessFailureStore) ListProjectGroupAccess(ctx context.Context, projectID string) ([]store.ProjectGroupAccessView, error) {
	if s.groupGrantsErr != nil {
		return nil, s.groupGrantsErr
	}
	return s.projectAccessCRUDStore.ListProjectGroupAccess(ctx, projectID)
}

func (s *projectAccessFailureStore) DeleteProjectUserAccess(ctx context.Context, projectID, userID string) error {
	if s.deleteUserErr != nil {
		return s.deleteUserErr
	}
	return s.projectAccessCRUDStore.DeleteProjectUserAccess(ctx, projectID, userID)
}

func (s *projectAccessFailureStore) DeleteProjectGroupAccess(ctx context.Context, projectID, groupID string) error {
	if s.deleteGroupErr != nil {
		return s.deleteGroupErr
	}
	return s.projectAccessCRUDStore.DeleteProjectGroupAccess(ctx, projectID, groupID)
}

func newProjectAccessFailureService(t *testing.T, fake *projectAccessFailureStore) *ProjectAccessService {
	t.Helper()
	service, err := NewProjectAccessService(New(fake), fake)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestProjectAccessApplicationPropagatesStoreFailures(t *testing.T) {
	const (
		projectID = "project-errors"
		adminID   = "admin-errors"
		userID    = "user-errors"
		groupID   = "group-errors"
	)
	opaque := errors.New("storage unavailable")
	base := newProjectAccessCRUDStore()
	base.roles[accessCRUDKey(projectID, adminID)] = store.ProjectRoleAdmin
	base.users[userID] = store.User{ID: userID, Username: "target", Status: store.UserStatusActive}
	base.groups[groupID] = store.Group{ID: groupID, Name: "target-group"}
	fake := &projectAccessFailureStore{projectAccessCRUDStore: base}
	service := newProjectAccessFailureService(t, fake)
	actor := activeProjectActor(adminID, store.DeploymentRoleMember)

	fake.effectiveErr = opaque
	if _, err := service.EffectiveRole(t.Context(), actor, projectID); !errors.Is(err, opaque) {
		t.Fatalf("EffectiveRole error = %v", err)
	}
	fake.effectiveErr = nil

	fake.userGrantsErr = opaque
	if _, err := service.ListUserAccess(t.Context(), actor, projectID); !errors.Is(err, opaque) {
		t.Fatalf("ListUserAccess error = %v", err)
	}
	fake.userGrantsErr = nil

	fake.groupGrantsErr = opaque
	if _, err := service.ListGroupAccess(t.Context(), actor, projectID); !errors.Is(err, opaque) {
		t.Fatalf("ListGroupAccess error = %v", err)
	}
	fake.groupGrantsErr = nil

	fake.getUserErr = opaque
	if _, err := service.UpsertUserAccess(t.Context(), actor, store.ProjectUserAccess{ProjectID: projectID, UserID: userID, Role: store.ProjectRoleViewer}); !errors.Is(err, opaque) {
		t.Fatalf("UpsertUserAccess error = %v", err)
	}
	fake.getUserErr = nil

	fake.listGroupsErr = opaque
	if _, err := service.UpsertGroupAccess(t.Context(), actor, store.ProjectGroupAccess{ProjectID: projectID, GroupID: groupID, Role: store.ProjectRoleViewer}); !errors.Is(err, opaque) {
		t.Fatalf("UpsertGroupAccess error = %v", err)
	}
	fake.listGroupsErr = nil

	fake.deleteUserErr = opaque
	if err := service.DeleteUserAccess(t.Context(), actor, projectID, userID); !errors.Is(err, opaque) {
		t.Fatalf("DeleteUserAccess error = %v", err)
	}
	fake.deleteUserErr = nil

	fake.deleteGroupErr = opaque
	if err := service.DeleteGroupAccess(t.Context(), actor, projectID, groupID); !errors.Is(err, opaque) {
		t.Fatalf("DeleteGroupAccess error = %v", err)
	}
}

func TestProjectAccessDirectoryCapsResultsAndPropagatesFailures(t *testing.T) {
	const (
		projectID = "project-directory-limit"
		adminID   = "admin-directory-limit"
	)
	opaque := errors.New("directory unavailable")
	base := newProjectAccessCRUDStore()
	base.roles[accessCRUDKey(projectID, adminID)] = store.ProjectRoleAdmin
	for i := 0; i < projectDirectoryLimit+10; i++ {
		userID := fmt.Sprintf("user-%02d", i)
		base.users[userID] = store.User{ID: userID, Username: fmt.Sprintf("user-%02d", i), Email: fmt.Sprintf("user-%02d@example.com", i), DisplayName: fmt.Sprintf("User %02d", i), Status: store.UserStatusActive}
		groupID := fmt.Sprintf("group-%02d", i)
		base.groups[groupID] = store.Group{ID: groupID, Name: fmt.Sprintf("group-%02d", i)}
	}
	base.users["disabled"] = store.User{ID: "disabled", Username: "disabled", Status: store.UserStatusDisabled}
	fake := &projectAccessFailureStore{projectAccessCRUDStore: base}
	service := newProjectAccessFailureService(t, fake)
	actor := activeProjectActor(adminID, store.DeploymentRoleMember)

	users, err := service.SearchUsers(t.Context(), actor, projectID, "")
	if err != nil || len(users) != projectDirectoryLimit {
		t.Fatalf("SearchUsers len=%d err=%v", len(users), err)
	}
	groups, err := service.SearchGroups(t.Context(), actor, projectID, "")
	if err != nil || len(groups) != projectDirectoryLimit {
		t.Fatalf("SearchGroups len=%d err=%v", len(groups), err)
	}

	fake.listUsersErr = opaque
	if _, err := service.SearchUsers(t.Context(), actor, projectID, "user"); !errors.Is(err, opaque) {
		t.Fatalf("SearchUsers store error = %v", err)
	}
	fake.listUsersErr = nil
	fake.listGroupsErr = opaque
	if _, err := service.SearchGroups(t.Context(), actor, projectID, "group"); !errors.Is(err, opaque) {
		t.Fatalf("SearchGroups store error = %v", err)
	}
}

func TestProjectAccessApplicationRejectsDisabledActors(t *testing.T) {
	fake := &projectAccessFailureStore{projectAccessCRUDStore: newProjectAccessCRUDStore()}
	service := newProjectAccessFailureService(t, fake)
	actor := AuthenticatedUser{ID: "disabled", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusDisabled}

	if _, err := service.ListProjects(t.Context(), actor); errorCode(err) != "forbidden" {
		t.Fatalf("ListProjects disabled actor error = %v", err)
	}
	if _, err := service.CreateProject(t.Context(), actor, projectAccessValidProject("", "Disabled")); errorCode(err) != "forbidden" {
		t.Fatalf("CreateProject disabled actor error = %v", err)
	}
}
