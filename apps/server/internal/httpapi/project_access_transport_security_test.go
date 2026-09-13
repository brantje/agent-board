package httpapi

import (
	"net/http"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectAuthorizationFailsClosedWhenAuthExistsWithoutProjectAccess(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	_, token := fixture.createUser(t, "partial-wiring-user", store.DeploymentRoleMember)

	handler := NewRouterWithApplication(&app.Services{
		ControlPlane: app.New(&fakeControlPlaneStore{}),
		Auth:         fixture.auth,
	})
	response := authHTTPRequest(t, handler, http.MethodGet, "/api/projects/"+projectID+"/issues", "", bearer(token))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("partial project authorization status=%d body=%s", response.Code, response.Body.String())
	}
	if !stringsContainsJSONCode(response.Body.String(), "project_authorization_unavailable") {
		t.Fatalf("partial project authorization body=%s", response.Body.String())
	}
}

func TestProjectAuthorizationResponsesDisableBrowserCaching(t *testing.T) {
	fixture := newProjectAccessHTTPFixture(t)
	admin, token := fixture.createUser(t, "cache-admin", store.DeploymentRoleMember)
	fixture.access.roles[projectGrantKey(projectID, admin.ID)] = store.ProjectRoleAdmin
	fixture.access.userGrants[projectGrantKey(projectID, admin.ID)] = store.ProjectUserAccess{ProjectID: projectID, UserID: admin.ID, Role: store.ProjectRoleAdmin}

	response := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/projects/"+projectID+"/access/users", "", bearer(token))
	if response.Code != http.StatusOK {
		t.Fatalf("access list status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control=%q, want private, no-store", got)
	}
}
