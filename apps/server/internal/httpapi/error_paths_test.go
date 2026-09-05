package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

func TestProjectScopedEndpointsHideMissingProject(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	modelBody := `{"providerId":"` + providerID + `","name":"Model","model":"model","generationSettings":{}}`
	runtimeBody := `{"name":"Runtime","kind":"docker","image":"image","networkPolicy":"none","capabilities":{}}`
	executorBody := `{"name":"Executor","engine":"test","modelProfileId":"` + modelID + `","runtimeId":"` + runtimeID + `","engineSettings":{}}`
	agentBody := `{"name":"Agent","executorProfileId":"` + executorID + `","concurrencyLimit":1,"state":"ENABLED"}`
	issueBody := `{"title":"Issue","status":"TODO"}`
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/projects/" + otherID, ""},
		{http.MethodPatch, "/api/projects/" + otherID, `{"name":"Renamed"}`},
		{http.MethodGet, "/api/projects/" + otherID + "/model-profiles", ""},
		{http.MethodPost, "/api/projects/" + otherID + "/model-profiles", modelBody},
		{http.MethodGet, "/api/projects/" + otherID + "/model-profiles/" + modelID, ""},
		{http.MethodPut, "/api/projects/" + otherID + "/model-profiles/" + modelID, modelBody},
		{http.MethodGet, "/api/projects/" + otherID + "/runtimes", ""},
		{http.MethodPost, "/api/projects/" + otherID + "/runtimes", runtimeBody},
		{http.MethodGet, "/api/projects/" + otherID + "/runtimes/" + runtimeID, ""},
		{http.MethodPut, "/api/projects/" + otherID + "/runtimes/" + runtimeID, runtimeBody},
		{http.MethodGet, "/api/projects/" + otherID + "/executor-profiles", ""},
		{http.MethodPost, "/api/projects/" + otherID + "/executor-profiles", executorBody},
		{http.MethodGet, "/api/projects/" + otherID + "/executor-profiles/" + executorID, ""},
		{http.MethodPut, "/api/projects/" + otherID + "/executor-profiles/" + executorID, executorBody},
		{http.MethodGet, "/api/projects/" + otherID + "/agents", ""},
		{http.MethodPost, "/api/projects/" + otherID + "/agents", agentBody},
		{http.MethodGet, "/api/projects/" + otherID + "/agents/" + agentID, ""},
		{http.MethodPut, "/api/projects/" + otherID + "/agents/" + agentID, agentBody},
		{http.MethodGet, "/api/projects/" + otherID + "/issues", ""},
		{http.MethodPost, "/api/projects/" + otherID + "/issues", issueBody},
		{http.MethodGet, "/api/projects/" + otherID + "/issues/" + issueID, ""},
		{http.MethodPatch, "/api/projects/" + otherID + "/issues/" + issueID, `{"status":"IN_PROGRESS"}`},
		{http.MethodPost, "/api/projects/" + otherID + "/issues/" + issueID + "/assignment", `{"agentId":"` + agentID + `"}`},
		{http.MethodGet, "/api/projects/" + otherID + "/runs", ""},
		{http.MethodGet, "/api/projects/" + otherID + "/runs/" + runID, ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "project_not_found") {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestGlobalEndpointValidationAndMissingResources(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	cases := []struct {
		method, path, body, code string
		status                   int
	}{
		{http.MethodPost, "/api/projects", `{}`, "invalid_argument", http.StatusBadRequest},
		{http.MethodPost, "/api/providers", `{}`, "invalid_argument", http.StatusBadRequest},
		{http.MethodPut, "/api/providers/" + otherID, `{"name":"Provider","kind":"test"}`, "provider_not_found", http.StatusNotFound},
		{http.MethodGet, "/api/model-profiles/" + otherID, "", "model_profile_not_found", http.StatusNotFound},
		{http.MethodGet, "/api/runtimes/" + otherID, "", "runtime_not_found", http.StatusNotFound},
		{http.MethodGet, "/api/executor-profiles/" + otherID, "", "executor_profile_not_found", http.StatusNotFound},
		{http.MethodGet, "/api/agents/" + otherID, "", "agent_not_found", http.StatusNotFound},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}
