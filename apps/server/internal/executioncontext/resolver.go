package executioncontext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type Store interface {
	GetProject(context.Context, string) (store.Project, error)
	GetIssue(context.Context, string, string) (store.Issue, error)
	GetRun(context.Context, string, string) (store.Run, error)
	GetWorkspace(context.Context, string, string) (store.Workspace, error)
	GetAgentInScope(context.Context, *string, string) (store.Agent, error)
	GetExecutorProfile(context.Context, *string, string) (store.ExecutorProfile, error)
	GetModelProfile(context.Context, *string, string) (store.ModelProfile, error)
	GetProvider(context.Context, string) (store.Provider, error)
	GetRuntime(context.Context, *string, string) (store.Runtime, error)
}

type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func AsError(err error) (*Error, bool) {
	var target *Error
	ok := errors.As(err, &target)
	return target, ok
}

func fail(code, message string, cause error) error {
	return &Error{Code: code, Message: message, Cause: cause}
}

type Resolver struct{ store Store }

func NewResolver(s Store) (*Resolver, error) {
	if s == nil {
		return nil, fmt.Errorf("execution context store is required")
	}
	return &Resolver{store: s}, nil
}

func (r *Resolver) Resolve(ctx context.Context, projectID, runID string) (Resolved, error) {
	project, err := r.store.GetProject(ctx, projectID)
	if err != nil {
		return Resolved{}, fail("execution_project_unavailable", "Project configuration is unavailable", err)
	}
	run, err := r.store.GetRun(ctx, projectID, runID)
	if err != nil {
		return Resolved{}, fail("execution_run_unavailable", "Run configuration is unavailable", err)
	}
	if run.ProjectID != projectID || run.AgentID == nil {
		return Resolved{}, fail("execution_configuration_invalid", "Run execution configuration is incomplete", nil)
	}
	issue, err := r.store.GetIssue(ctx, projectID, run.IssueID)
	if err != nil {
		return Resolved{}, fail("execution_issue_unavailable", "Issue configuration is unavailable", err)
	}
	workspace, err := r.store.GetWorkspace(ctx, projectID, run.WorkspaceID)
	if err != nil {
		return Resolved{}, fail("execution_workspace_unavailable", "Workspace configuration is unavailable", err)
	}
	if issue.ProjectID != projectID || workspace.ProjectID != projectID || workspace.IssueID != issue.ID || run.IssueID != issue.ID {
		return Resolved{}, fail("execution_configuration_invalid", "Run, Issue and Workspace configuration is inconsistent", nil)
	}

	scope := &projectID
	agent, err := r.store.GetAgentInScope(ctx, scope, *run.AgentID)
	if err != nil {
		return Resolved{}, fail("execution_agent_unavailable", "Agent configuration is unavailable", err)
	}
	if agent.State != "ENABLED" || !scopeAllows(agent.ProjectID, projectID) {
		return Resolved{}, fail("execution_agent_unavailable", "Agent is not available for execution", nil)
	}
	executor, err := r.store.GetExecutorProfile(ctx, scope, agent.ExecutorProfileID)
	if err != nil {
		return Resolved{}, fail("execution_executor_unavailable", "Executor Profile configuration is unavailable", err)
	}
	if !executor.Enabled || !scopeAllows(executor.ProjectID, projectID) {
		return Resolved{}, fail("execution_executor_unavailable", "Executor Profile is not available for execution", nil)
	}
	model, err := r.store.GetModelProfile(ctx, scope, executor.ModelProfileID)
	if err != nil {
		return Resolved{}, fail("execution_model_unavailable", "Model Profile configuration is unavailable", err)
	}
	if !model.Enabled || !scopeAllows(model.ProjectID, projectID) {
		return Resolved{}, fail("execution_model_unavailable", "Model Profile is not available for execution", nil)
	}
	provider, err := r.store.GetProvider(ctx, model.ProviderID)
	if err != nil {
		return Resolved{}, fail("execution_provider_unavailable", "Provider configuration is unavailable", err)
	}
	if !provider.Enabled {
		return Resolved{}, fail("execution_provider_unavailable", "Provider is not available for execution", nil)
	}
	runtime, err := r.store.GetRuntime(ctx, scope, executor.RuntimeID)
	if err != nil {
		return Resolved{}, fail("execution_runtime_unavailable", "Runtime configuration is unavailable", err)
	}
	if !runtime.Enabled || !scopeAllows(runtime.ProjectID, projectID) {
		return Resolved{}, fail("execution_runtime_unavailable", "Runtime is not available for execution", nil)
	}

	return Resolved{
		Safe: SafeContext{
			Project: ProjectContext{ID: project.ID, Name: project.Name, RepositoryPath: project.RepositoryPath, DefaultBranch: project.DefaultBranch, WorkflowSettings: cloneJSON(project.WorkflowSettings)},
			Issue: IssueContext{ID: issue.ID, Title: issue.Title, Description: issue.Description, Status: issue.Status},
			Run: RunContext{ID: run.ID, Attempt: run.Attempt},
			Agent: AgentContext{ID: agent.ID, Name: agent.Name, RoleInstructions: agent.RoleInstructions},
			Executor: ExecutorContext{ID: executor.ID, Name: executor.Name, Engine: executor.Engine, EngineSettings: cloneJSON(executor.EngineSettings)},
			Model: ModelContext{ID: model.ID, Name: model.Name, Model: model.Model, Temperature: cloneFloat64(model.Temperature), MaxTokens: cloneInt(model.MaxTokens), GenerationSettings: cloneJSON(model.GenerationSettings)},
			Provider: ProviderContext{ID: provider.ID, Name: provider.Name, Kind: provider.Kind, BaseURL: cloneString(provider.BaseURL), SafeMetadata: cloneJSON(provider.SafeMetadata)},
			Runtime: RuntimeContext{ID: runtime.ID, Name: runtime.Name, Kind: runtime.Kind, Image: runtime.Image, CPULimitMillis: cloneInt(runtime.CPULimitMillis), MemoryLimitBytes: cloneInt64(runtime.MemoryLimitBytes), PIDLimit: cloneInt(runtime.PIDLimit), TimeoutSeconds: cloneInt(runtime.TimeoutSeconds), NetworkPolicy: runtime.NetworkPolicy, WorkspacePolicy: runtime.WorkspacePolicy, Capabilities: cloneJSON(runtime.Capabilities)},
			Workspace: WorkspaceContext{ID: workspace.ID, Path: workspace.Path, RepositoryPath: cloneString(workspace.RepositoryPath), BaseBranch: cloneString(workspace.BaseBranch), BaseRevision: cloneString(workspace.BaseRevision), WorkingBranch: workspace.WorkingBranch, BootstrapStatus: workspace.BootstrapStatus},
		},
		ProviderCredentialRef: cloneString(provider.CredentialRef),
		AllowedSecretRefs:     append([]string(nil), runtime.AllowedSecretRefs...),
	}, nil
}

func scopeAllows(scope *string, projectID string) bool { return scope == nil || *scope == projectID }

func cloneJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
