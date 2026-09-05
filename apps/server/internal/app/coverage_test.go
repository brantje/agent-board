package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func coverageProjectID() string { return "project" }
func coverageScope() *string    { v := coverageProjectID(); return &v }
func coverageProvider() store.Provider {
	return store.Provider{ID: "provider", Name: "Provider", Kind: "test", Enabled: true, HealthStatus: "UNKNOWN", SafeMetadata: store.EmptyObject}
}
func coverageModel() store.ModelProfile {
	return store.ModelProfile{ID: "model", ProjectID: coverageScope(), ProviderID: "provider", Name: "Model", Model: "model", GenerationSettings: store.EmptyObject, Enabled: true}
}
func coverageRuntime() store.Runtime {
	return store.Runtime{ID: "runtime", ProjectID: coverageScope(), Name: "Runtime", Kind: "docker", Image: "image", NetworkPolicy: "none", WorkspacePolicy: "issue", Capabilities: store.EmptyObject, Enabled: true, HealthStatus: "UNKNOWN"}
}
func coverageExecutor() store.ExecutorProfile {
	return store.ExecutorProfile{ID: "executor", ProjectID: coverageScope(), Name: "Executor", Engine: "test", ModelProfileID: "model", RuntimeID: "runtime", EngineSettings: store.EmptyObject, Enabled: true}
}
func coverageIssue() store.Issue {
	return store.Issue{ID: "issue", ProjectID: coverageProjectID(), Title: "Issue", Status: "TODO"}
}
func coverageRun() store.Run {
	a := "agent"
	return store.Run{ID: "run", ProjectID: coverageProjectID(), IssueID: "issue", WorkspaceID: "workspace", AgentID: &a, Attempt: 1, Status: "QUEUED"}
}

func (f *fakeStore) ListProjects(context.Context) ([]store.Project, error) {
	return []store.Project{f.project}, nil
}
func (f *fakeStore) CreateProject(_ context.Context, v store.Project) (store.Project, error) { return v, nil }
func (f *fakeStore) UpdateProject(_ context.Context, v store.Project) (store.Project, error) { return v, nil }
func (f *fakeStore) ListProviders(context.Context) ([]store.Provider, error) {
	return []store.Provider{coverageProvider()}, nil
}
func (f *fakeStore) CreateProvider(_ context.Context, v store.Provider) (store.Provider, error) { return v, nil }
func (f *fakeStore) GetProvider(context.Context, string) (store.Provider, error) { return coverageProvider(), nil }
func (f *fakeStore) UpdateProvider(_ context.Context, v store.Provider) (store.Provider, error) { return v, nil }
func (f *fakeStore) ListModelProfiles(context.Context, *string) ([]store.ModelProfile, error) {
	return []store.ModelProfile{coverageModel()}, nil
}
func (f *fakeStore) CreateModelProfile(_ context.Context, v store.ModelProfile) (store.ModelProfile, error) { return v, nil }
func (f *fakeStore) GetModelProfile(context.Context, *string, string) (store.ModelProfile, error) { return coverageModel(), nil }
func (f *fakeStore) UpdateModelProfile(_ context.Context, _ *string, v store.ModelProfile) (store.ModelProfile, error) { return v, nil }
func (f *fakeStore) ListRuntimes(context.Context, *string) ([]store.Runtime, error) {
	return []store.Runtime{coverageRuntime()}, nil
}
func (f *fakeStore) CreateRuntime(_ context.Context, v store.Runtime) (store.Runtime, error) { return v, nil }
func (f *fakeStore) GetRuntime(context.Context, *string, string) (store.Runtime, error) { return coverageRuntime(), nil }
func (f *fakeStore) UpdateRuntime(_ context.Context, _ *string, v store.Runtime) (store.Runtime, error) { return v, nil }
func (f *fakeStore) ListExecutorProfiles(context.Context, *string) ([]store.ExecutorProfile, error) {
	return []store.ExecutorProfile{coverageExecutor()}, nil
}
func (f *fakeStore) CreateExecutorProfile(_ context.Context, v store.ExecutorProfile) (store.ExecutorProfile, error) { return v, nil }
func (f *fakeStore) GetExecutorProfile(context.Context, *string, string) (store.ExecutorProfile, error) { return coverageExecutor(), nil }
func (f *fakeStore) UpdateExecutorProfile(_ context.Context, _ *string, v store.ExecutorProfile) (store.ExecutorProfile, error) { return v, nil }
func (f *fakeStore) ListAgents(context.Context, *string) ([]store.Agent, error) { return []store.Agent{f.agent}, nil }
func (f *fakeStore) CreateAgent(_ context.Context, v store.Agent) (store.Agent, error) { return v, nil }
func (f *fakeStore) UpdateAgent(_ context.Context, _ *string, v store.Agent) (store.Agent, error) { return v, nil }
func (f *fakeStore) ListIssues(context.Context, string) ([]store.Issue, error) { return []store.Issue{coverageIssue()}, nil }
func (f *fakeStore) CreateIssue(_ context.Context, v store.Issue) (store.Issue, error) { return v, nil }
func (f *fakeStore) GetIssue(context.Context, string, string) (store.Issue, error) { return coverageIssue(), nil }
func (f *fakeStore) UpdateIssue(_ context.Context, v store.Issue) (store.Issue, error) { return v, nil }
func (f *fakeStore) ListRuns(context.Context, string) ([]store.Run, error) { return []store.Run{coverageRun()}, nil }
func (f *fakeStore) GetRun(context.Context, string, string) (store.Run, error) { return coverageRun(), nil }
func (f *fakeStore) AssignIssue(context.Context, string, string, string) (store.Issue, store.Run, error) {
	i := coverageIssue()
	i.Status = "IN_PROGRESS"
	return i, coverageRun(), nil
}

func TestControlPlaneServiceHappyPaths(t *testing.T) {
	ctx := context.Background()
	pid := coverageProjectID()
	scope := &pid
	f := &fakeStore{project: store.Project{ID: pid, Name: "Project", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: store.EmptyObject}, agent: store.Agent{ID: "agent", ProjectID: scope, Name: "Agent", ExecutorProfileID: "executor", ConcurrencyLimit: 1, State: "ENABLED"}}
	svc := New(f)
	p := f.project
	provider := coverageProvider()
	model := coverageModel()
	runtime := coverageRuntime()
	executor := coverageExecutor()
	agent := f.agent
	issue := coverageIssue()
	calls := []func() error{
		func() error { _, e := svc.ListProjects(ctx); return e }, func() error { _, e := svc.CreateProject(ctx, p); return e }, func() error { _, e := svc.GetProject(ctx, p.ID); return e }, func() error { _, e := svc.UpdateProject(ctx, p); return e },
		func() error { _, e := svc.ListProviders(ctx); return e }, func() error { _, e := svc.CreateProvider(ctx, provider); return e }, func() error { _, e := svc.GetProvider(ctx, provider.ID); return e }, func() error { _, e := svc.UpdateProvider(ctx, provider); return e },
		func() error { _, e := svc.ListModelProfiles(ctx, nil); return e }, func() error { _, e := svc.ListModelProfiles(ctx, scope); return e }, func() error { _, e := svc.GetModelProfile(ctx, scope, model.ID); return e }, func() error { _, e := svc.CreateModelProfile(ctx, model); return e }, func() error { _, e := svc.UpdateModelProfile(ctx, scope, model); return e },
		func() error { _, e := svc.ListRuntimes(ctx, scope); return e }, func() error { _, e := svc.GetRuntime(ctx, scope, runtime.ID); return e }, func() error { _, e := svc.CreateRuntime(ctx, runtime); return e }, func() error { _, e := svc.UpdateRuntime(ctx, scope, runtime); return e },
		func() error { _, e := svc.ListExecutorProfiles(ctx, scope); return e }, func() error { _, e := svc.GetExecutorProfile(ctx, scope, executor.ID); return e }, func() error { _, e := svc.CreateExecutorProfile(ctx, executor); return e }, func() error { _, e := svc.UpdateExecutorProfile(ctx, scope, executor); return e },
		func() error { _, e := svc.ListAgents(ctx, scope); return e }, func() error { _, e := svc.GetAgent(ctx, scope, agent.ID); return e }, func() error { _, e := svc.CreateAgent(ctx, agent); return e }, func() error { _, e := svc.UpdateAgent(ctx, scope, agent); return e },
		func() error { _, e := svc.ListIssues(ctx, pid); return e }, func() error { _, e := svc.GetIssue(ctx, pid, issue.ID); return e }, func() error { _, e := svc.CreateIssue(ctx, issue); return e }, func() error { _, e := svc.UpdateIssue(ctx, issue); return e }, func() error { _, e := svc.ListRuns(ctx, pid); return e }, func() error { _, e := svc.GetRun(ctx, pid, "run"); return e }, func() error { _, _, e := svc.AssignIssue(ctx, pid, issue.ID, agent.ID); return e },
	}
	for n, call := range calls {
		if err := call(); err != nil { t.Fatalf("call %d: %v", n, err) }
	}
}

type missingProjectStore struct{ store.ControlPlaneStore }
func (*missingProjectStore) GetProject(context.Context, string) (store.Project, error) { return store.Project{}, store.ErrNotFound }

func TestControlPlaneServiceScopeAndValidationErrors(t *testing.T) {
	ctx := context.Background()
	pid := coverageProjectID()
	scope := &pid
	svc := New(&missingProjectStore{})
	scopeCalls := []func() error{
		func() error { _, e := svc.ListModelProfiles(ctx, scope); return e }, func() error { _, e := svc.GetRuntime(ctx, scope, "runtime"); return e }, func() error { _, e := svc.ListExecutorProfiles(ctx, scope); return e }, func() error { _, e := svc.GetAgent(ctx, scope, "agent"); return e }, func() error { _, e := svc.ListIssues(ctx, pid); return e }, func() error { _, e := svc.GetRun(ctx, pid, "run"); return e },
	}
	for _, call := range scopeCalls {
		err := call()
		ae, ok := AsError(err)
		if !ok || ae.Code != "project_not_found" { t.Fatalf("scope error=%v", err) }
	}
	good := &fakeStore{project: store.Project{ID: pid}, agent: store.Agent{ID: "agent"}}
	s := New(good)
	badCalls := []func() error{
		func() error { _, e := s.CreateProject(ctx, store.Project{}); return e }, func() error { _, e := s.UpdateProject(ctx, store.Project{}); return e }, func() error { _, e := s.CreateProvider(ctx, store.Provider{}); return e }, func() error { _, e := s.UpdateProvider(ctx, store.Provider{}); return e }, func() error { _, e := s.CreateModelProfile(ctx, store.ModelProfile{}); return e }, func() error { _, e := s.UpdateModelProfile(ctx, nil, store.ModelProfile{}); return e }, func() error { _, e := s.CreateRuntime(ctx, store.Runtime{}); return e }, func() error { _, e := s.UpdateRuntime(ctx, nil, store.Runtime{}); return e }, func() error { _, e := s.CreateExecutorProfile(ctx, store.ExecutorProfile{}); return e }, func() error { _, e := s.UpdateExecutorProfile(ctx, nil, store.ExecutorProfile{}); return e }, func() error { _, e := s.CreateAgent(ctx, store.Agent{}); return e }, func() error { _, e := s.UpdateAgent(ctx, nil, store.Agent{}); return e }, func() error { _, e := s.CreateIssue(ctx, store.Issue{ProjectID: pid}); return e }, func() error { _, e := s.UpdateIssue(ctx, store.Issue{ProjectID: pid}); return e },
	}
	for _, call := range badCalls {
		ae, ok := AsError(call())
		if !ok || ae.Code != "invalid_argument" { t.Fatalf("validation error=%v", ae) }
	}
}

func TestValidatorsAndStoreErrorTranslation(t *testing.T) {
	zero := 0
	low := -1.0
	high := 3.0
	invalids := []error{
		validateProject(store.Project{}), validateProject(store.Project{Name: "p", RepositoryPath: "/r", DefaultBranch: " ", WorkflowSettings: store.EmptyObject}), validateProject(store.Project{Name: "p", RepositoryPath: "/r", WorkflowSettings: json.RawMessage(`[]`)}),
		validateProvider(store.Provider{}), validateProvider(store.Provider{Name: "p", Kind: "k", SafeMetadata: json.RawMessage(`[]`)}),
		validateModelProfile(store.ModelProfile{}), validateModelProfile(store.ModelProfile{ProviderID: "p", Name: "m", Model: "m", Temperature: &low}), validateModelProfile(store.ModelProfile{ProviderID: "p", Name: "m", Model: "m", Temperature: &high}), validateModelProfile(store.ModelProfile{ProviderID: "p", Name: "m", Model: "m", MaxTokens: &zero}), validateModelProfile(store.ModelProfile{ProviderID: "p", Name: "m", Model: "m", MaxConcurrent: &zero}), validateModelProfile(store.ModelProfile{ProviderID: "p", Name: "m", Model: "m", GenerationSettings: json.RawMessage(`[]`)}),
		validateRuntime(store.Runtime{}), validateRuntime(store.Runtime{Name: "r", Kind: "docker", Image: "i", NetworkPolicy: "bad"}), validateRuntime(store.Runtime{Name: "r", Kind: "docker", Image: "i", NetworkPolicy: "none", WorkspacePolicy: "bad"}), validateRuntime(store.Runtime{Name: "r", Kind: "docker", Image: "i", NetworkPolicy: "none", Capabilities: json.RawMessage(`[]`)}),
		validateExecutorProfile(store.ExecutorProfile{}), validateExecutorProfile(store.ExecutorProfile{Name: "e", Engine: "e", ModelProfileID: "m", RuntimeID: "r", EngineSettings: json.RawMessage(`[]`)}),
		validateAgent(store.Agent{}), validateAgent(store.Agent{Name: "a", ExecutorProfileID: "e", ConcurrencyLimit: 0, State: "ENABLED"}), validateAgent(store.Agent{Name: "a", ExecutorProfileID: "e", ConcurrencyLimit: 1, State: "BAD"}), validateIssue(store.Issue{Status: "TODO"}), validateIssue(store.Issue{Title: "i", Status: "BAD"}),
	}
	for _, err := range invalids {
		if ae, ok := AsError(err); !ok || ae.Code != "invalid_argument" { t.Fatalf("invalid=%v", err) }
	}
	for _, tc := range []struct{ err error; code string }{{store.ErrNotFound, "issue_not_found"}, {store.ErrConflict, "conflict"}, {store.ErrInvalidArgument, "invalid_argument"}} {
		ae, ok := AsError(translateStoreError(tc.err, "issue"))
		if !ok || ae.Code != tc.code || !errors.Is(ae, tc.err) { t.Fatalf("translation=%v", ae) }
	}
	if translateStoreError(nil, "issue") != nil { t.Fatal("nil translation") }
	raw := errors.New("raw")
	if !errors.Is(translateStoreError(raw, "issue"), raw) { t.Fatal("raw translation") }
}

type doneIssueStore struct{ *fakeStore }
func (s *doneIssueStore) GetIssue(context.Context, string, string) (store.Issue, error) { i := coverageIssue(); i.Status = "DONE"; return i, nil }
type disabledExecutorStore struct{ *fakeStore }
func (s *disabledExecutorStore) GetExecutorProfile(context.Context, *string, string) (store.ExecutorProfile, error) { e := coverageExecutor(); e.Enabled = false; return e, nil }

func TestAssignmentPreflightErrorsAndAppErrorString(t *testing.T) {
	pid := coverageProjectID()
	scope := &pid
	base := &fakeStore{project: store.Project{ID: pid}, agent: store.Agent{ID: "agent", ProjectID: scope, Name: "Agent", ExecutorProfileID: "executor", ConcurrencyLimit: 1, State: "ENABLED"}}
	for _, tc := range []struct{ svc *Service; code string }{{New(&doneIssueStore{fakeStore: base}), "issue_done"}, {New(&disabledExecutorStore{fakeStore: base}), "execution_configuration_invalid"}} {
		_, _, err := tc.svc.AssignIssue(context.Background(), pid, "issue", "agent")
		ae, ok := AsError(err)
		if !ok || ae.Code != tc.code { t.Fatalf("assignment error=%v", err) }
	}
	err := NewError("code", "message", store.ErrConflict)
	if err.Error() != "message" || !errors.Is(err, store.ErrConflict) { t.Fatalf("error=%v", err) }
}

type disabledAgentStore struct{ *fakeStore }
func (s *disabledAgentStore) GetAgentInScope(context.Context, *string, string) (store.Agent, error) { a := s.agent; a.State = "DISABLED"; return a, nil }
type disabledModelStore struct{ *fakeStore }
func (s *disabledModelStore) GetModelProfile(context.Context, *string, string) (store.ModelProfile, error) { m := coverageModel(); m.Enabled = false; return m, nil }
type disabledProviderStore struct{ *fakeStore }
func (s *disabledProviderStore) GetProvider(context.Context, string) (store.Provider, error) { p := coverageProvider(); p.Enabled = false; return p, nil }
type disabledRuntimeStore struct{ *fakeStore }
func (s *disabledRuntimeStore) GetRuntime(context.Context, *string, string) (store.Runtime, error) { r := coverageRuntime(); r.Enabled = false; return r, nil }

func TestAssignmentRejectsUnavailableConfiguration(t *testing.T) {
	pid := coverageProjectID()
	scope := &pid
	makeBase := func() *fakeStore { return &fakeStore{project: store.Project{ID: pid}, agent: store.Agent{ID: "agent", ProjectID: scope, Name: "Agent", ExecutorProfileID: "executor", ConcurrencyLimit: 1, State: "ENABLED"}} }
	cases := []*Service{New(&disabledAgentStore{fakeStore: makeBase()}), New(&disabledModelStore{fakeStore: makeBase()}), New(&disabledProviderStore{fakeStore: makeBase()}), New(&disabledRuntimeStore{fakeStore: makeBase()})}
	for _, svc := range cases {
		_, _, err := svc.AssignIssue(context.Background(), pid, "issue", "agent")
		ae, ok := AsError(err)
		if !ok || (ae.Code != "agent_unavailable" && ae.Code != "execution_configuration_invalid") { t.Fatalf("assignment error=%v", err) }
	}
}
