package httpapi

import (
	"net/http"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	projectAccessGroupID        = "44444444-4444-4444-8444-444444444444"
	projectAccessOtherGroupID   = "55555555-5555-4555-8555-555555555555"
	projectAccessUnknownUserID  = "66666666-6666-4666-8666-666666666666"
	projectAccessUnknownGroupID = "77777777-7777-4777-8777-777777777777"
)

func projectAdminFixture(t *testing.T) (*projectAccessHTTPFixture, store.User, string) {
	t.Helper()
	fixture := newProjectAccessHTTPFixture(t)
	admin, token := fixture.createUser(t, "access-admin", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, admin.ID)] = store.ProjectRoleAdmin
	fixture.access.userGrants[projectGrantKey(projectID, admin.ID)] = store.ProjectUserAccess{
		ProjectID: projectID,
		UserID:    admin.ID,
		Role:      store.ProjectRoleAdmin,
	}
	return fixture, admin, token
}

func TestProjectAccessHTTPAdminManagesDirectUserAndGroupGrants(t *testing.T) {
	fixture, _, token := projectAdminFixture(t)
	target, _ := fixture.createUser(t, "grant-target", store.DeploymentRoleMember)
	fixture.access.groups[projectAccessGroupID] = store.Group{ID: projectAccessGroupID, Name: "backend"}

	userGrant := authHTTPRequest(t, fixture.handler, http.MethodPut,
		"/api/projects/"+projectID+"/access/users/"+target.ID,
		`{"role":"member"}`, bearer(token))
	if userGrant.Code != http.StatusOK || fixture.access.userGrants[projectGrantKey(projectID, target.ID)].Role != store.ProjectRoleMember {
		t.Fatalf("user grant status=%d body=%s grant=%+v", userGrant.Code, userGrant.Body.String(), fixture.access.userGrants[projectGrantKey(projectID, target.ID)])
	}

	users := authHTTPRequest(t, fixture.handler, http.MethodGet,
		"/api/projects/"+projectID+"/access/users", "", bearer(token))
	if users.Code != http.StatusOK || !stringsContainsJSONCode(users.Body.String(), "grant-target") || !stringsContainsJSONCode(users.Body.String(), store.ProjectRoleMember) {
		t.Fatalf("list users status=%d body=%s", users.Code, users.Body.String())
	}

	groupGrant := authHTTPRequest(t, fixture.handler, http.MethodPut,
		"/api/projects/"+projectID+"/access/groups/"+projectAccessGroupID,
		`{"role":"viewer"}`, bearer(token))
	if groupGrant.Code != http.StatusOK || fixture.access.groupGrants[projectGrantKey(projectID, projectAccessGroupID)].Role != store.ProjectRoleViewer {
		t.Fatalf("group grant status=%d body=%s grant=%+v", groupGrant.Code, groupGrant.Body.String(), fixture.access.groupGrants[projectGrantKey(projectID, projectAccessGroupID)])
	}

	groups := authHTTPRequest(t, fixture.handler, http.MethodGet,
		"/api/projects/"+projectID+"/access/groups", "", bearer(token))
	if groups.Code != http.StatusOK || !stringsContainsJSONCode(groups.Body.String(), "backend") || !stringsContainsJSONCode(groups.Body.String(), store.ProjectRoleViewer) {
		t.Fatalf("list groups status=%d body=%s", groups.Code, groups.Body.String())
	}

	deleteGroup := authHTTPRequest(t, fixture.handler, http.MethodDelete,
		"/api/projects/"+projectID+"/access/groups/"+projectAccessGroupID, "", bearer(token))
	if deleteGroup.Code != http.StatusNoContent {
		t.Fatalf("delete group status=%d body=%s", deleteGroup.Code, deleteGroup.Body.String())
	}
	if _, ok := fixture.access.groupGrants[projectGrantKey(projectID, projectAccessGroupID)]; ok {
		t.Fatal("group grant still present after delete")
	}

	deleteUser := authHTTPRequest(t, fixture.handler, http.MethodDelete,
		"/api/projects/"+projectID+"/access/users/"+target.ID, "", bearer(token))
	if deleteUser.Code != http.StatusNoContent {
		t.Fatalf("delete user status=%d body=%s", deleteUser.Code, deleteUser.Body.String())
	}
	if _, ok := fixture.access.userGrants[projectGrantKey(projectID, target.ID)]; ok {
		t.Fatal("user grant still present after delete")
	}
}

func TestProjectAccessHTTPDirectorySearchReturnsOnlySafeActiveIdentity(t *testing.T) {
	fixture, _, token := projectAdminFixture(t)
	active, _ := fixture.createUser(t, "alice-search", store.DeploymentRoleMember)
	disabled, _ := fixture.createUser(t, "alice-disabled", store.DeploymentRoleMember)
	disabled.Status = store.UserStatusDisabled
	disabled.PasswordHash = "must-not-leak"
	disabled.AuthVersion = 99
	fixture.access.users[disabled.ID] = disabled
	fixture.access.groups[projectAccessGroupID] = store.Group{ID: projectAccessGroupID, Name: "backend"}
	fixture.access.groups[projectAccessOtherGroupID] = store.Group{ID: projectAccessOtherGroupID, Name: "frontend"}

	users := authHTTPRequest(t, fixture.handler, http.MethodGet,
		"/api/projects/"+projectID+"/access/directory/users?q=alice", "", bearer(token))
	if users.Code != http.StatusOK || !stringsContainsJSONCode(users.Body.String(), active.ID) {
		t.Fatalf("directory users status=%d body=%s", users.Code, users.Body.String())
	}
	if stringsContainsJSONCode(users.Body.String(), disabled.ID) || stringsContainsJSONCode(users.Body.String(), "must-not-leak") || stringsContainsJSONCode(users.Body.String(), "authVersion") {
		t.Fatalf("directory users leaked disabled/security fields: %s", users.Body.String())
	}

	groups := authHTTPRequest(t, fixture.handler, http.MethodGet,
		"/api/projects/"+projectID+"/access/directory/groups?q=back", "", bearer(token))
	if groups.Code != http.StatusOK || !stringsContainsJSONCode(groups.Body.String(), projectAccessGroupID) || stringsContainsJSONCode(groups.Body.String(), projectAccessOtherGroupID) {
		t.Fatalf("directory groups status=%d body=%s", groups.Code, groups.Body.String())
	}
}

func TestProjectAccessHTTPListsDisabledAttachedUsersWithoutSecurityFields(t *testing.T) {
	fixture, _, token := projectAdminFixture(t)
	disabled, _ := fixture.createUser(t, "disabled-attached", store.DeploymentRoleMember)
	disabled.Status = store.UserStatusDisabled
	disabled.PasswordHash = "secret-hash"
	disabled.AuthVersion = 17
	fixture.access.users[disabled.ID] = disabled
	fixture.access.userGrants[projectGrantKey(projectID, disabled.ID)] = store.ProjectUserAccess{
		ProjectID: projectID,
		UserID:    disabled.ID,
		Role:      store.ProjectRoleViewer,
	}

	response := authHTTPRequest(t, fixture.handler, http.MethodGet,
		"/api/projects/"+projectID+"/access/users", "", bearer(token))
	if response.Code != http.StatusOK || !stringsContainsJSONCode(response.Body.String(), disabled.ID) || !stringsContainsJSONCode(response.Body.String(), store.UserStatusDisabled) {
		t.Fatalf("disabled grant status=%d body=%s", response.Code, response.Body.String())
	}
	if stringsContainsJSONCode(response.Body.String(), "secret-hash") || stringsContainsJSONCode(response.Body.String(), "authVersion") {
		t.Fatalf("disabled grant leaked security fields: %s", response.Body.String())
	}
}

func TestProjectAccessHTTPRejectsInvalidRolesAndUnknownSubjects(t *testing.T) {
	fixture, _, token := projectAdminFixture(t)

	invalidRole := authHTTPRequest(t, fixture.handler, http.MethodPut,
		"/api/projects/"+projectID+"/access/users/"+projectAccessUnknownUserID,
		`{"role":"owner"}`, bearer(token))
	if invalidRole.Code != http.StatusBadRequest || !stringsContainsJSONCode(invalidRole.Body.String(), "invalid_argument") {
		t.Fatalf("invalid role status=%d body=%s", invalidRole.Code, invalidRole.Body.String())
	}

	unknownUser := authHTTPRequest(t, fixture.handler, http.MethodPut,
		"/api/projects/"+projectID+"/access/users/"+projectAccessUnknownUserID,
		`{"role":"viewer"}`, bearer(token))
	if unknownUser.Code != http.StatusNotFound || !stringsContainsJSONCode(unknownUser.Body.String(), "user_not_found") {
		t.Fatalf("unknown user status=%d body=%s", unknownUser.Code, unknownUser.Body.String())
	}

	unknownGroup := authHTTPRequest(t, fixture.handler, http.MethodPut,
		"/api/projects/"+projectID+"/access/groups/"+projectAccessUnknownGroupID,
		`{"role":"member"}`, bearer(token))
	if unknownGroup.Code != http.StatusNotFound || !stringsContainsJSONCode(unknownGroup.Body.String(), "group_not_found") {
		t.Fatalf("unknown group status=%d body=%s", unknownGroup.Code, unknownGroup.Body.String())
	}
}
