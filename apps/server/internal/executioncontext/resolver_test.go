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
	model     store.ModelProfile
	provider  store.Provider
}

func (f fakeStore) GetProject(context.Context, string) (store.Project, error) { return f.project, nil }
func (f fakeStore) GetIssue(context.Context, string, string) (store.Issue, error) {
	return f.issue, nil
}
func (f fakeStore) GetRun(context.Context, string, string) (store.Run, error) { return f.run, nil }
func (f fakeStore) GetWorkspace(context.Context, string, string) (store.Workspace, error) {
	return f.workspace, nil
}
func (f fakeStore) GetAgentInScope(context.Context, *string, string) (store.Agent, error) {
	return f.agent, nil
}
func (f fakeStore) GetModelProfile(context.Context, *string, string) (store.ModelProfile, error) {
	return f.model, nil
}
func (f fakeStore) GetProvider(context.Context, *string, string) (store.Provider, error) {
	return f.provider, nil
}

func validStore() fakeStore {
	projectID := "p1"
	agentID := "a1"
	credentialRef := "provider-token"
	baseURL := "https://example.test"
	return fakeStore{
		project:   store.Project{ID: projectID, Name: "Project", RepositoryPath: "/repo", DefaultBranch: "main", WorkflowSettings: json.RawMessage(`{"mode":"review"}`)},
		issue:     store.Issue{ID: "i1", ProjectID: projectID, Title: "Issue", Description: "Do work", Status: "IN_PROGRESS"},
		run:       store.Run{ID: "r1", ProjectID: projectID, IssueID: "i1", WorkspaceID: "w1", AgentID: &agentID, Attempt: 2},
		workspace: store.Workspace{ID: "w1", ProjectID: projectID, IssueID: "i1", Path: "/work/w1", WorkingBranch: "issue/i1", BootstrapStatus: "READY"},
		agent:     store.Agent{ID: agentID, ProjectID: &projectID, Name: "Coder", RoleInstructions: "Implement", Engine: "opencode", ModelProfileID: "m1", EngineSettings: json.RawMessage(`{}`), State: "ENABLED"},
		model:     store.ModelProfile{ID: "m1", ProjectID: &projectID, ProviderID: "pr1", Name: "model", Model: "gpt", Enabled: true},
		provider:  store.Provider{ID: "pr1", Name: "provider", Kind: "openai-compatible", BaseURL: &baseURL, CredentialRef: &credentialRef, Enabled: true},
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
	if got.Safe.Agent.RoleInstructions != "Implement" || got.Safe.Agent.Engine != "opencode" {
		t.Fatalf("resolved = %+v", got.Safe)
	}
	if got.ProviderCredentialRef == nil || *got.ProviderCredentialRef != "provider-token" {
		t.Fatalf("credential ref = %v", got.ProviderCredentialRef)
	}
}

type runnerAwareStore struct {
	fakeStore
	sessions []store.ExecutionSession
	runner   store.Runner
}

func (s runnerAwareStore) ListExecutionSessionsByRun(_ context.Context, projectID, runID string, _ []string) ([]store.ExecutionSession, error) {
	if projectID != s.project.ID || runID != s.run.ID {
		return nil, store.ErrNotFound
	}
	return append([]store.ExecutionSession(nil), s.sessions...), nil
}

func (s runnerAwareStore) GetRunner(_ context.Context, id string) (store.Runner, error) {
	if id != s.runner.ID {
		return store.Runner{}, store.ErrNotFound
	}
	return s.runner, nil
}

func TestResolveAttachesSelectedRunnerFromExecutionSession(t *testing.T) {
	values := validStore()
	resolver, err := NewResolver(runnerAwareStore{
		fakeStore: values,
		sessions: []store.ExecutionSession{{
			ID: "session-1", ProjectID: values.project.ID, RunID: values.run.ID, RunnerID: "runner-1", Status: "PENDING",
		}},
		runner: store.Runner{ID: "runner-1", Name: "lab-host", Internal: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver.Resolve(context.Background(), "p1", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Safe.Runner == nil || got.Safe.Runner.ID != "runner-1" || got.Safe.Runner.Name != "lab-host" || got.Safe.Runner.Internal {
		t.Fatalf("runner=%+v", got.Safe.Runner)
	}
}

func TestResolveRejectsForeignScopedConfiguration(t *testing.T) {
	values := validStore()
	foreign := "other"
	values.provider.ProjectID = &foreign
	resolver, err := NewResolver(values)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), "p1", "r1")
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_provider_unavailable" {
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
