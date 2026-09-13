package httpapi

import (
	"net/http"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestPhase5ProtectedSurfacesRequireAuthentication(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	for _, path := range []string{
		"/api/projects",
		"/api/providers",
		"/api/runners",
		"/api/secrets",
	} {
		t.Run(path, func(t *testing.T) {
			response := authHTTPRequest(t, fixture.handler, http.MethodGet, path, "", nil)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPhase5NestedProjectResourcesPreserveNotFoundIsolation(t *testing.T) {
	fixture, _, token := projectAdminFixture(t)
	for _, path := range []string{
		"/api/projects/" + otherID + "/issues/" + issueID,
		"/api/projects/" + otherID + "/runs/" + runID,
		"/api/projects/" + otherID + "/questions/" + questionID,
		"/api/projects/" + otherID + "/reviews/" + reviewID,
		"/api/projects/" + otherID + "/events",
	} {
		t.Run(path, func(t *testing.T) {
			response := authHTTPRequest(t, fixture.handler, http.MethodGet, path, "", bearer(token))
			if response.Code != http.StatusNotFound || !stringsContainsJSONCode(response.Body.String(), "project_not_found") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPhase5ProjectRoleChangesApplyOnNextAuthoritativeRequest(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	user, token := fixture.createUser(t, "role-change-user", store.DeploymentRoleMember)
	key := projectGrantKey(projectID, user.ID)
	fixture.access.roles[key] = store.ProjectRoleViewer

	blocked := authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects/"+projectID+"/issues", `{"title":"blocked"}`, bearer(token))
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation status=%d body=%s", blocked.Code, blocked.Body.String())
	}

	fixture.access.roles[key] = store.ProjectRoleMember
	allowed := authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects/"+projectID+"/issues", `{"title":"allowed"}`, bearer(token))
	if allowed.Code != http.StatusCreated {
		t.Fatalf("member mutation status=%d body=%s", allowed.Code, allowed.Body.String())
	}

	fixture.access.roles[key] = store.ProjectRoleViewer
	blockedAgain := authHTTPRequest(t, fixture.handler, http.MethodPost, "/api/projects/"+projectID+"/issues", `{"title":"blocked again"}`, bearer(token))
	if blockedAgain.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation after downgrade status=%d body=%s", blockedAgain.Code, blockedAgain.Body.String())
	}
}

func TestPhase5DisabledAndForcedPasswordUsersCannotUseProtectedProjectAccess(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	member, memberToken := fixture.createUser(t, "lifecycle-member", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, member.ID)] = store.ProjectRoleViewer

	_, deploymentAdminToken := fixture.createUser(t, "lifecycle-admin", store.DeploymentRoleAdmin)
	deploymentAdmin, err := fixture.auth.AuthenticateNormalAccess(t.Context(), deploymentAdminToken)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.auth.AdminSetDisabled(t.Context(), deploymentAdmin, member.ID, true); err != nil {
		t.Fatal(err)
	}
	disabled := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects", "", bearer(memberToken))
	if disabled.Code != http.StatusUnauthorized {
		t.Fatalf("disabled access status=%d body=%s", disabled.Code, disabled.Body.String())
	}

	if _, err := fixture.auth.AdminSetDisabled(t.Context(), deploymentAdmin, member.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.auth.AdminSetPassword(t.Context(), deploymentAdmin, member.ID, "admin-assigned-long-password"); err != nil {
		t.Fatal(err)
	}
	forcedTokens, err := fixture.auth.Login(t.Context(), member.Username, "admin-assigned-long-password")
	if err != nil {
		t.Fatal(err)
	}
	forced := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects", "", bearer(forcedTokens.AccessToken))
	if forced.Code != http.StatusForbidden || !stringsContainsJSONCode(forced.Body.String(), "password_change_required") {
		t.Fatalf("forced-password access status=%d body=%s", forced.Code, forced.Body.String())
	}
}
