package app

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectAccessAdministrationOperationsRejectProjectMember(t *testing.T) {
	const (
		projectID = "project-admin-boundary"
		memberID  = "member-admin-boundary"
	)
	fake := newProjectAccessCRUDStore()
	fake.roles[accessCRUDKey(projectID, memberID)] = store.ProjectRoleMember
	service := newProjectAccessCRUDService(t, fake)
	actor := activeProjectActor(memberID, store.DeploymentRoleMember)

	assertForbidden := func(name string, err error) {
		t.Helper()
		if errorCode(err) != "forbidden" {
			t.Fatalf("%s error=%v", name, err)
		}
	}

	_, err := service.ListUserAccess(t.Context(), actor, projectID)
	assertForbidden("ListUserAccess", err)
	_, err = service.UpsertUserAccess(t.Context(), actor, store.ProjectUserAccess{ProjectID: projectID, UserID: "user-1", Role: store.ProjectRoleViewer})
	assertForbidden("UpsertUserAccess", err)
	assertForbidden("DeleteUserAccess", service.DeleteUserAccess(t.Context(), actor, projectID, "user-1"))

	_, err = service.ListGroupAccess(t.Context(), actor, projectID)
	assertForbidden("ListGroupAccess", err)
	_, err = service.UpsertGroupAccess(t.Context(), actor, store.ProjectGroupAccess{ProjectID: projectID, GroupID: "group-1", Role: store.ProjectRoleViewer})
	assertForbidden("UpsertGroupAccess", err)
	assertForbidden("DeleteGroupAccess", service.DeleteGroupAccess(t.Context(), actor, projectID, "group-1"))

	_, err = service.SearchUsers(t.Context(), actor, projectID, "user")
	assertForbidden("SearchUsers", err)
	_, err = service.SearchGroups(t.Context(), actor, projectID, "group")
	assertForbidden("SearchGroups", err)
}

func TestProjectAccessAdministrationMapsGrantStoreErrors(t *testing.T) {
	const (
		projectID = "project-admin-store-errors"
		adminID   = "admin-store-errors"
		userID    = "user-store-errors"
		groupID   = "group-store-errors"
	)
	base := newProjectAccessCRUDStore()
	base.roles[accessCRUDKey(projectID, adminID)] = store.ProjectRoleAdmin
	base.users[userID] = store.User{ID: userID, Username: "user-store-errors", Status: store.UserStatusActive}
	base.groups[groupID] = store.Group{ID: groupID, Name: "group-store-errors"}
	service := newProjectAccessCRUDService(t, base)
	actor := activeProjectActor(adminID, store.DeploymentRoleMember)

	if err := service.DeleteUserAccess(t.Context(), actor, projectID, userID); errorCode(err) != "project_access_not_found" {
		t.Fatalf("DeleteUserAccess missing grant error=%v", err)
	}
	if err := service.DeleteGroupAccess(t.Context(), actor, projectID, groupID); errorCode(err) != "project_access_not_found" {
		t.Fatalf("DeleteGroupAccess missing grant error=%v", err)
	}
}
