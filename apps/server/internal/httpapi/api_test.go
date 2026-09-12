package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	projectID       = "11111111-1111-4111-8111-111111111111"
	providerID      = "22222222-2222-4222-8222-222222222222"
	modelID         = "33333333-3333-4333-8333-333333333333"
	runtimeID       = "44444444-4444-4444-8444-444444444444"
	agentID         = "66666666-6666-4666-8666-666666666666"
	issueID         = "77777777-7777-4777-8777-777777777777"
	issueKey        = "AB-1"
	otherIssueKey   = "AB-2"
	missingIssueKey = "AB-99"
	runID           = "88888888-8888-4888-8888-888888888888"
	workspaceID     = "99999999-9999-4999-8999-999999999999"
	otherID         = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

type fakeControlPlaneStore struct{ store.ControlPlaneStore }

func scoped(scope *string) *string {
	if scope == nil {
		return nil
	}
	v := *scope
	return &v
}
func issueFixture(status string) store.Issue {
	return store.Issue{ID: issueID, ProjectID: projectID, Key: issueKey, Number: 1, Title: "Issue", Status: status}
}

func (f *fakeControlPlaneStore) ListProjects(context.Context) ([]store.Project, error) {
	return []store.Project{{ID: projectID, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}}, nil
}
func (f *fakeControlPlaneStore) CreateProject(_ context.Context, v store.Project) (store.Project, error) {
	v.ID = projectID
	if v.IssuePrefix == "" {
		v.IssuePrefix = "AB"
	}
	if v.WorkflowSettings == nil {
		v.WorkflowSettings = store.EmptyObject
	}
	return v, nil
}
func (f *fakeControlPlaneStore) GetProject(_ context.Context, id string) (store.Project, error) {
	if id != projectID {
		return store.Project{}, store.ErrNotFound
	}
	return store.Project{ID: projectID, Name: "Project", IssuePrefix: "AB", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, nil
}
func (f *fakeControlPlaneStore) UpdateProject(_ context.Context, v store.Project) (store.Project, error) {
	return v, nil
}
func (f *fakeControlPlaneStore) ListProviders(_ context.Context, scope *string) ([]store.Provider, error) {
	return []store.Provider{{ID: providerID, ProjectID: scoped(scope), Name: "Provider", Kind: "test", Enabled: true, HealthStatus: "UNKNOWN", SafeMetadata: store.EmptyObject}}, nil
}
func (f *fakeControlPlaneStore) CreateProvider(_ context.Context, v store.Provider) (store.Provider, error) {
	v.ID = providerID
	v.HealthStatus = "UNKNOWN"
	if v.SafeMetadata == nil {
		v.SafeMetadata = store.EmptyObject
	}
	return v, nil
}
func (f *fakeControlPlaneStore) GetProvider(_ context.Context, scope *string, id string) (store.Provider, error) {
	if id != providerID {
		return store.Provider{}, store.ErrNotFound
	}
	secret := "secret-ref"
	return store.Provider{ID: providerID, ProjectID: scoped(scope), Name: "Provider", Kind: "test", CredentialRef: &secret, Enabled: true, HealthStatus: "UNKNOWN", SafeMetadata: store.EmptyObject}, nil
}
func (f *fakeControlPlaneStore) UpdateProvider(_ context.Context, scope *string, v store.Provider) (store.Provider, error) {
	if scope == nil {
		if v.ProjectID != nil {
			return store.Provider{}, store.ErrNotFound
		}
		return v, nil
	}
	if v.ProjectID == nil || *v.ProjectID != *scope {
		return store.Provider{}, store.ErrNotFound
	}
	return v, nil
}
func (f *fakeControlPlaneStore) ListAllProviders(_ context.Context) ([]store.Provider, error) {
	return []store.Provider{{ID: providerID, Name: "Provider", Kind: "test", Enabled: true, HealthStatus: "UNKNOWN", SafeMetadata: store.EmptyObject}}, nil
}
func (f *fakeControlPlaneStore) UpdateProviderHealth(context.Context, string, string, *int, *int) error {
	return nil
}
func (f *fakeControlPlaneStore) ListModelProfiles(_ context.Context, scope *string) ([]store.ModelProfile, error) {
	return []store.ModelProfile{{ID: modelID, ProjectID: scoped(scope), ProviderID: providerID, Name: "Model", Model: "model", GenerationSettings: store.EmptyObject, Enabled: true}}, nil
}
func (f *fakeControlPlaneStore) CreateModelProfile(_ context.Context, v store.ModelProfile) (store.ModelProfile, error) {
	v.ID = modelID
	if v.GenerationSettings == nil {
		v.GenerationSettings = store.EmptyObject
	}
	return v, nil
}
func (f *fakeControlPlaneStore) GetModelProfile(_ context.Context, scope *string, id string) (store.ModelProfile, error) {
	if id != modelID {
		return store.ModelProfile{}, store.ErrNotFound
	}
	return store.ModelProfile{ID: modelID, ProjectID: scoped(scope), ProviderID: providerID, Name: "Model", Model: "model", GenerationSettings: store.EmptyObject, Enabled: true}, nil
}
func (f *fakeControlPlaneStore) UpdateModelProfile(_ context.Context, _ *string, v store.ModelProfile) (store.ModelProfile, error) {
	return v, nil
}
func (f *fakeControlPlaneStore) ListRuntimes(_ context.Context, scope *string) ([]store.Runtime, error) {
	return []store.Runtime{{ID: runtimeID, ProjectID: scoped(scope), Name: "Runtime", Kind: "docker", Image: "image", NetworkPolicy: "none", WorkspacePolicy: "issue", Capabilities: store.EmptyObject, Enabled: true, HealthStatus: "UNKNOWN"}}, nil
}
func (f *fakeControlPlaneStore) CreateRuntime(_ context.Context, v store.Runtime) (store.Runtime, error) {
	v.ID = runtimeID
	v.HealthStatus = "UNKNOWN"
	if v.Capabilities == nil {
		v.Capabilities = store.EmptyObject
	}
	return v, nil
}
func (f *fakeControlPlaneStore) GetRuntime(_ context.Context, scope *string, id string) (store.Runtime, error) {
	if id != runtimeID {
		return store.Runtime{}, store.ErrNotFound
	}
	return store.Runtime{ID: runtimeID, ProjectID: scoped(scope), Name: "Runtime", Kind: "docker", Image: "image", NetworkPolicy: "none", WorkspacePolicy: "issue", Capabilities: store.EmptyObject, Enabled: true, HealthStatus: "UNKNOWN"}, nil
}
func (f *fakeControlPlaneStore) UpdateRuntime(_ context.Context, _ *string, v store.Runtime) (store.Runtime, error) {
	return v, nil
}
func agentFixture(scope *string) store.Agent {
	return store.Agent{ID: agentID, ProjectID: scoped(scope), Name: "Agent", Engine: "test", ModelProfileID: modelID, EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, State: "ENABLED"}
}
func (f *fakeControlPlaneStore) ListAgents(_ context.Context, scope *string) ([]store.Agent, error) {
	return []store.Agent{agentFixture(scope)}, nil
}
func (f *fakeControlPlaneStore) CreateAgent(_ context.Context, v store.Agent) (store.Agent, error) {
	v.ID = agentID
	if v.EngineSettings == nil {
		v.EngineSettings = store.EmptyObject
	}
	return v, nil
}
func (f *fakeControlPlaneStore) GetAgentInScope(_ context.Context, scope *string, id string) (store.Agent, error) {
	if id != agentID {
		return store.Agent{}, store.ErrNotFound
	}
	return agentFixture(scope), nil
}
func (f *fakeControlPlaneStore) UpdateAgent(_ context.Context, _ *string, v store.Agent) (store.Agent, error) {
	return v, nil
}
func (f *fakeControlPlaneStore) CreateIssue(_ context.Context, v store.Issue) (store.Issue, error) {
	v.ID = issueID
	v.Key = issueKey
	v.Number = 1
	return v, nil
}
func (f *fakeControlPlaneStore) GetIssue(_ context.Context, pid, id string) (store.Issue, error) {
	if pid != projectID || id != issueID {
		return store.Issue{}, store.ErrNotFound
	}
	return issueFixture("TODO"), nil
}
func (f *fakeControlPlaneStore) GetIssueUUIDByKey(_ context.Context, pid, key string) (string, error) {
	if pid != projectID {
		return "", store.ErrNotFound
	}
	switch key {
	case issueKey:
		return issueID, nil
	case otherIssueKey:
		return otherID, nil
	default:
		return "", store.ErrNotFound
	}
}
func (f *fakeControlPlaneStore) ListProjectEventsAfter(_ context.Context, pid, afterID string, limit int) ([]store.Event, error) {
	if pid != projectID {
		return nil, store.ErrNotFound
	}
	if afterID != "" {
		return nil, store.ErrNotFound
	}
	return nil, nil
}
func (f *fakeControlPlaneStore) ListIssues(_ context.Context, pid string) ([]store.Issue, error) {
	if pid != projectID {
		return nil, store.ErrNotFound
	}
	return []store.Issue{
		issueFixture("TODO"),
		{ID: otherID, ProjectID: projectID, Key: otherIssueKey, Number: 2, Title: "Other", Status: "BACKLOG"},
	}, nil
}
func (f *fakeControlPlaneStore) UpdateIssue(_ context.Context, v store.Issue) (store.Issue, error) {
	return v, nil
}
func (f *fakeControlPlaneStore) ListRuns(_ context.Context, pid string) ([]store.Run, error) {
	if pid != projectID {
		return nil, store.ErrNotFound
	}
	return []store.Run{{ID: runID, ProjectID: projectID, IssueID: issueID, WorkspaceID: workspaceID, AgentID: stringPtr(agentID), Attempt: 1, Status: "QUEUED"}}, nil
}
func (f *fakeControlPlaneStore) GetRun(_ context.Context, pid, id string) (store.Run, error) {
	if pid != projectID || id != runID {
		return store.Run{}, store.ErrNotFound
	}
	return store.Run{ID: runID, ProjectID: projectID, IssueID: issueID, WorkspaceID: workspaceID, AgentID: stringPtr(agentID), Attempt: 1, Status: "QUEUED"}, nil
}
func (f *fakeControlPlaneStore) AssignIssue(context.Context, string, string, string) (store.Issue, store.Run, error) {
	issue := issueFixture("IN_PROGRESS")
	issue.AssignedAgentID = stringPtr(agentID)
	return issue, store.Run{ID: runID, ProjectID: projectID, IssueID: issueID, WorkspaceID: workspaceID, AgentID: stringPtr(agentID), Attempt: 1, Status: "QUEUED"}, nil
}
func stringPtr(v string) *string { return &v }

func TestControlPlaneRoutes(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	projectBody := `{"name":"Project","issuePrefix":"AB","repositoryPath":"/repo","workflowSettings":{}}`
	providerBody := `{"name":"Provider","kind":"test","credentialRef":"secret-ref","safeMetadata":{}}`
	modelBody := `{"providerId":"` + providerID + `","name":"Model","model":"model","generationSettings":{}}`
	runtimeBody := `{"name":"Runtime","kind":"docker","image":"image","networkPolicy":"none","capabilities":{}}`
	agentBody := `{"name":"Agent","engine":"test","modelProfileId":"` + modelID + `","engineSettings":{},"concurrencyLimit":1,"state":"ENABLED"}`
	issueBody := `{"title":"Issue","status":"TODO"}`
	cases := []struct {
		name, method, path, body string
		status                   int
	}{
		{"list projects", "GET", "/api/projects", "", 200}, {"create project", "POST", "/api/projects", projectBody, 201}, {"get project", "GET", "/api/projects/" + projectID, "", 200}, {"update project", "PATCH", "/api/projects/" + projectID, `{"name":"Renamed"}`, 200},
	}
	resources := []struct{ name, path, id, body string }{{"provider", "providers", providerID, providerBody}, {"model", "model-profiles", modelID, modelBody}, {"runtime", "runtimes", runtimeID, runtimeBody}, {"agent", "agents", agentID, agentBody}}
	for _, r := range resources {
		cases = append(cases,
			struct {
				name, method, path, body string
				status                   int
			}{"list global " + r.name, "GET", "/api/" + r.path, "", 200},
			struct {
				name, method, path, body string
				status                   int
			}{"create global " + r.name, "POST", "/api/" + r.path, r.body, 201},
			struct {
				name, method, path, body string
				status                   int
			}{"get global " + r.name, "GET", "/api/" + r.path + "/" + r.id, "", 200},
			struct {
				name, method, path, body string
				status                   int
			}{"update global " + r.name, "PUT", "/api/" + r.path + "/" + r.id, r.body, 200},
			struct {
				name, method, path, body string
				status                   int
			}{"list project " + r.name, "GET", "/api/projects/" + projectID + "/" + r.path, "", 200},
			struct {
				name, method, path, body string
				status                   int
			}{"create project " + r.name, "POST", "/api/projects/" + projectID + "/" + r.path, r.body, 201},
			struct {
				name, method, path, body string
				status                   int
			}{"get project " + r.name, "GET", "/api/projects/" + projectID + "/" + r.path + "/" + r.id, "", 200},
			struct {
				name, method, path, body string
				status                   int
			}{"update project " + r.name, "PUT", "/api/projects/" + projectID + "/" + r.path + "/" + r.id, r.body, 200},
		)
	}
	cases = append(cases,
		struct {
			name, method, path, body string
			status                   int
		}{"list issues", "GET", "/api/projects/" + projectID + "/issues", "", 200},
		struct {
			name, method, path, body string
			status                   int
		}{"create issue", "POST", "/api/projects/" + projectID + "/issues", issueBody, 201},
		struct {
			name, method, path, body string
			status                   int
		}{"get issue", "GET", "/api/projects/" + projectID + "/issues/" + issueKey, "", 200},
		struct {
			name, method, path, body string
			status                   int
		}{"update issue", "PATCH", "/api/projects/" + projectID + "/issues/" + issueKey, `{"status":"IN_PROGRESS"}`, 200},
		struct {
			name, method, path, body string
			status                   int
		}{"assign issue", "POST", "/api/projects/" + projectID + "/issues/" + issueKey + "/assignment", `{"agentId":"` + agentID + `"}`, 202},
		struct {
			name, method, path, body string
			status                   int
		}{"list runs", "GET", "/api/projects/" + projectID + "/runs", "", 200},
		struct {
			name, method, path, body string
			status                   int
		}{"get run", "GET", "/api/projects/" + projectID + "/runs/" + runID, "", 200},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("content type=%q", rec.Header().Get("Content-Type"))
			}
			if strings.Contains(tc.name, "create") && strings.Contains(tc.name, "provider") && (strings.Contains(rec.Body.String(), "credential") || strings.Contains(rec.Body.String(), "secret-ref")) {
				t.Fatalf("provider response leaked credential: %s", rec.Body.String())
			}
		})
	}
}

func TestControlPlaneRejectsInvalidRequestsAndInaccessibleIDs(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	cases := []struct {
		name, method, path, body, code string
		status                         int
	}{
		{"invalid project id", "GET", "/api/projects/not-a-uuid", "", "invalid_id", 400},
		{"unknown field", "POST", "/api/projects", `{"name":"P","issuePrefix":"AB","repositoryPath":"/repo","unknown":true}`, "invalid_request", 400},
		{"multiple objects", "POST", "/api/projects", `{"name":"P","issuePrefix":"AB","repositoryPath":"/repo"} {}`, "invalid_request", 400},
		{"invalid runtime", "POST", "/api/runtimes", `{"name":"R","kind":"podman","image":"i","networkPolicy":"none"}`, "invalid_argument", 400},
		{"cross project issue", "GET", "/api/projects/" + projectID + "/issues/" + missingIssueKey, "", "issue_not_found", 404},
		{"uuid issue id rejected", "GET", "/api/projects/" + projectID + "/issues/" + issueID, "", "invalid_id", 400},
		{"missing provider", "GET", "/api/providers/" + otherID, "", "provider_not_found", 404},
		{"invalid assignment agent", "POST", "/api/projects/" + projectID + "/issues/" + issueKey + "/assignment", `{"agentId":"bad"}`, "invalid_id", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.code) {
				t.Fatalf("expected code %q in %s", tc.code, rec.Body.String())
			}
		})
	}
}

func TestOpenAPICoversEveryAPIRoute(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatal("router does not expose chi routes")
	}
	specRoot := filepath.Join("..", "..", "..", "..", "packages", "api")
	mainBytes, err := os.ReadFile(filepath.Join(specRoot, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	refs := parsePathRefs(string(mainBytes))
	err = chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/") {
			return nil
		}
		ref, exists := refs[route]
		if !exists {
			return &routeError{method: method, route: route, reason: "missing path"}
		}
		parts := strings.SplitN(ref, "#/", 2)
		if len(parts) != 2 {
			return &routeError{method: method, route: route, reason: "invalid ref"}
		}
		data, readErr := os.ReadFile(filepath.Join(specRoot, strings.TrimPrefix(parts[0], "./")))
		if readErr != nil {
			return readErr
		}
		if !blockHasMethod(string(data), parts[1], strings.ToLower(method)) {
			return &routeError{method: method, route: route, reason: "missing method"}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type routeError struct{ method, route, reason string }

func (e *routeError) Error() string { return e.method + " " + e.route + ": " + e.reason }
func parsePathRefs(spec string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(spec, "\n")
	var route string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(line, "  /api/") && strings.HasSuffix(trim, ":") {
			route = strings.TrimSuffix(trim, ":")
			continue
		}
		if route != "" && strings.HasPrefix(trim, "$ref: ") {
			out[route] = strings.TrimSpace(strings.TrimPrefix(trim, "$ref: "))
			route = ""
		}
	}
	return out
}
func blockHasMethod(doc, label, method string) bool {
	lines := strings.Split(doc, "\n")
	in := false
	for _, line := range lines {
		if line == label+":" {
			in = true
			continue
		}
		if in && len(line) > 0 && line[0] != ' ' {
			break
		}
		if in && line == "  "+method+":" {
			return true
		}
	}
	return false
}

func TestDTOsMarshalAsObjects(t *testing.T) {
	for _, value := range []any{projectDTO(store.Project{IssuePrefix: "AB", WorkflowSettings: store.EmptyObject}), issueDTO(issueFixture("TODO")), providerDTO(store.Provider{SafeMetadata: store.EmptyObject}), modelProfileDTO(store.ModelProfile{GenerationSettings: store.EmptyObject}), runtimeDTO(store.Runtime{Capabilities: store.EmptyObject}), agentDTO(store.Agent{EngineSettings: store.EmptyObject}), runDTO(store.Run{IssueID: issueID}, map[string]string{issueID: issueKey})} {
		if _, err := json.Marshal(value); err != nil {
			t.Fatal(err)
		}
	}
}

func TestControlPlaneRejectsOversizedBody(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	body := `{"name":"` + strings.Repeat("a", (1<<20)+32) + `","repositoryPath":"/repo"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/projects", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
