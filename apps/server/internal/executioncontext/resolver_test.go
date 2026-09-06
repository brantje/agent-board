package executioncontext

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type fakeStore struct {
	project   store.Project
	issue     store.Issue
	run       store.Run
	workspace store.Workspace
	agent     store.Agent
	executor  store.ExecutorProfile
	model     store.ModelProfile
	provider  store.Provider
	runtime   store.Runtime
}

func (f fakeStore) GetProject(context.Context, string) (store.Project, error) { return f.project, nil }
func (f fakeStore) GetIssue(context.Context, string, string) (store.Issue, error) { return f.issue, nil }
func (f fakeStore) GetRun(context.Context, string, string) (store.Run, error) { return f.run, nil }
func (f fakeStore) GetWorkspace(context.Context, string, string) (store.Workspace, error) { return f.workspace, nil }
func (f fakeStore) GetAgentInScope(context.Context, *string, string) (store.Agent, error) { return f.agent, nil }
func (f fakeStore) GetExecutorProfile(context.Context, *string, string) (store.ExecutorProfile, error) { return f.executor, nil }
func (f fakeStore) GetModelProfile(context.Context, *string, string) (store.ModelProfile, error) { return f.model, nil }
func (f fakeStore) GetProvider(context.Context, string) (store.Provider, error) { return f.provider, nil }
func (f fakeStore) GetRuntime(context.Context, *string, string) (store.Runtime, error) { return f.runtime, nil }

func validStore() fakeStore {
	projectID := "p1"
	agentID := "a1"
	credentialRef := "provider-token"
	baseURL := "https://example.test"
	return fakeStore{
		project: store.Project{ID: projectID, Name: "Project", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: json.RawMessage(`{"mode":"review"}`)},
		issue: store.Issue{ID: "i1", ProjectID: projectID, Title: "Issue", Description: "Do work", Status: "IN_PROGRESS"},
		run: store.Run{ID: "r1", ProjectID: projectID, IssueID: "i1", WorkspaceID: "w1", AgentID: &agentID, Attempt: 2},
		workspace: store.Workspace{ID: "w1", ProjectID: projectID, IssueID: "i1", Path: "/work/w1", WorkingBranch: "issue/i1", BootstrapStatus: "READY"},
		agent: store.Agent{ID: agentID, ProjectID: &projectID, Name: "Coder", RoleInstructions: "Implement", ExecutorProfileID: "e1", State: "ENABLED"},
		executor: store.ExecutorProfile{ID: "e1", ProjectID: &projectID, Name: "exec", Engine: "opencode", ModelProfileID: "m1", RuntimeID: "rt1", Enabled: true},
		model: store.ModelProfile{ID: "m1", ProjectID: &projectID, ProviderID: "pr1", Name: "model", Model: "gpt", Enabled: true},
		provider: store.Provider{ID: "pr1", Name: "provider", Kind: "openai-compatible", BaseURL: &baseURL, CredentialRef: &credentialRef, Enabled: true},
		runtime: store.Runtime{ID: "rt1", ProjectID: &projectID, Name: "runtime", Kind: "docker", Image: "runtime:test", NetworkPolicy: "restricted", WorkspacePolicy: "issue", AllowedSecretRefs: []string{"runtime-token"}, Enabled: true},
	}
}

func TestResolveBuildsSafeImmutableContext(t *testing.T) {
	values := validStore()
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver.Resolve(context.Background(), "p1", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Safe.Agent.RoleInstructions != "Implement" || got.Safe.Runtime.Image != "runtime:test" {
		t.Fatalf("resolved = %+v", got.Safe)
	}
	if got.ProviderCredentialRef == nil || *got.ProviderCredentialRef != "provider-token" {
		t.Fatalf("credential ref = %v", got.ProviderCredentialRef)
	}
	values.runtime.AllowedSecretRefs[0] = "changed"
	if got.AllowedSecretRefs[0] != "runtime-token" {
		t.Fatal("resolved secret refs alias mutable store data")
	}
}

func TestResolveRejectsForeignScopedConfiguration(t *testing.T) {
	values := validStore()
	foreign := "other"
	values.runtime.ProjectID = &foreign
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), "p1", "r1")
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_runtime_unavailable" {
		t.Fatalf("err = %#v", err)
	}
}

func TestResolveRejectsDisabledConfiguration(t *testing.T) {
	values := validStore()
	values.model.Enabled = false
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), "p1", "r1")
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_model_unavailable" {
		t.Fatalf("err = %#v", err)
	}
}

func TestResolverDefensiveHelpers(t *testing.T) {
	if _, err := NewResolver(nil); err == nil {
		t.Fatal("expected nil store to be rejected")
	}
	if got := (&Error{Message: "safe execution failure"}).Error(); got != "safe execution failure" {
		t.Fatalf("Error() = %q", got)
	}

	intValue := 7
	if got := cloneInt(&intValue); got == nil || *got != intValue || got == &intValue {
		t.Fatalf("cloneInt() = %v", got)
	}
	if cloneInt(nil) != nil {
		t.Fatal("cloneInt(nil) must remain nil")
	}

	int64Value := int64(9)
	if got := cloneInt64(&int64Value); got == nil || *got != int64Value || got == &int64Value {
		t.Fatalf("cloneInt64() = %v", got)
	}
	if cloneInt64(nil) != nil {
		t.Fatal("cloneInt64(nil) must remain nil")
	}

	floatValue := 0.25
	if got := cloneFloat64(&floatValue); got == nil || *got != floatValue || got == &floatValue {
		t.Fatalf("cloneFloat64() = %v", got)
	}
	if cloneFloat64(nil) != nil {
		t.Fatal("cloneFloat64(nil) must remain nil")
	}
}
