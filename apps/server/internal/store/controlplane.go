package store

import "context"

type ControlPlaneStore interface {
	CoreStore
	ConfigurationStore
	ExecutionStore
	SchedulerStore
	EvidenceStore
	RuntimeAcquisitionStore

	ListProjects(context.Context) ([]Project, error)
	UpdateProject(context.Context, Project) (Project, error)
	ListIssues(context.Context, string) ([]Issue, error)
	UpdateIssue(context.Context, Issue) (Issue, error)
	ListRuns(context.Context, string) ([]Run, error)
	AssignIssue(context.Context, string, string, string) (Issue, Run, error)

	ListProviders(context.Context) ([]Provider, error)
	GetProvider(context.Context, string) (Provider, error)
	UpdateProvider(context.Context, Provider) (Provider, error)

	ListModelProfiles(context.Context, *string) ([]ModelProfile, error)
	GetModelProfile(context.Context, *string, string) (ModelProfile, error)
	UpdateModelProfile(context.Context, *string, ModelProfile) (ModelProfile, error)

	ListRuntimes(context.Context, *string) ([]Runtime, error)
	GetRuntime(context.Context, *string, string) (Runtime, error)
	UpdateRuntime(context.Context, *string, Runtime) (Runtime, error)

	ListExecutorProfiles(context.Context, *string) ([]ExecutorProfile, error)
	GetExecutorProfile(context.Context, *string, string) (ExecutorProfile, error)
	UpdateExecutorProfile(context.Context, *string, ExecutorProfile) (ExecutorProfile, error)

	ListAgents(context.Context, *string) ([]Agent, error)
	GetAgentInScope(context.Context, *string, string) (Agent, error)
	UpdateAgent(context.Context, *string, Agent) (Agent, error)
}
