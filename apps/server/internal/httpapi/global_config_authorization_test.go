package httpapi

import (
	"net/http"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestGlobalConfigurationRequiresDeploymentAdmin(t *testing.T) {
	fixture, _, projectAdminToken := projectAdminFixture(t)

	for _, path := range []string{
		"/api/repository-settings",
		"/api/providers",
		"/api/model-profiles",
		"/api/runtimes",
		"/api/agents",
		"/api/runners",
		"/api/secrets",
	} {
		t.Run(path, func(t *testing.T) {
			response := authHTTPRequest(t, fixture.handler, http.MethodGet, path, "", bearer(projectAdminToken))
			if response.Code != http.StatusForbidden {
				t.Fatalf("project admin global config status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	_, deploymentAdminToken := fixture.createUser(t, "global-config-admin", store.DeploymentRoleAdmin)
	response := authHTTPRequest(t, fixture.handler, http.MethodGet, "/api/providers", "", bearer(deploymentAdminToken))
	if response.Code != http.StatusOK {
		t.Fatalf("deployment admin global config status=%d body=%s", response.Code, response.Body.String())
	}
}
