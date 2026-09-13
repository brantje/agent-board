package httpapi

import (
	"net/http"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectAccessHTTPCallerControlledIdentityHeadersDoNotAuthenticate(t *testing.T) {
	fixture, admin, _ := projectAdminFixture(t)

	response := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID, "", map[string]string{
		"X-User-ID":         admin.ID,
		"X-Deployment-Role": store.DeploymentRoleAdmin,
		"X-Project-Role":    store.ProjectRoleAdmin,
	})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("spoofed identity headers status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProjectAccessHTTPCallerHeadersCannotElevateAuthenticatedUser(t *testing.T) {
	fixture, admin, _ := projectAdminFixture(t)
	_, unrelatedToken := fixture.createUser(t, "header-spoof-unrelated", store.DeploymentRoleMember)

	response := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID, "", map[string]string{
		"Authorization":     "Bearer " + unrelatedToken,
		"X-User-ID":         admin.ID,
		"X-Deployment-Role": store.DeploymentRoleAdmin,
		"X-Project-Role":    store.ProjectRoleAdmin,
	})
	if response.Code != http.StatusNotFound || !stringsContainsJSONCode(response.Body.String(), "project_not_found") {
		t.Fatalf("spoofed authenticated identity status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProjectAccessHTTPProjectIDCannotBypassScope(t *testing.T) {
	fixture, _, token := projectAdminFixture(t)

	response := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+otherID+"/issues/"+issueID, "", bearer(token))
	if response.Code != http.StatusNotFound || !stringsContainsJSONCode(response.Body.String(), "project_not_found") {
		t.Fatalf("cross-project URL status=%d body=%s", response.Code, response.Body.String())
	}
}
