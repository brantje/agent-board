package store

import "context"

type ControlPlaneStore interface {
	CoreStore
	ConfigurationStore
	ExecutionStore
	SchedulerStore
	EvidenceStore

	ListProjects(context.Context) ([]Project, error)
	UpdateProject(context.Context, Project) (Project, error)
	ListIssues(context.Context, string) ([]Issue, error)
	UpdateIssue(context.Context, Issue) (Issue, error)
	ListIssueRelationships(context.Context, string, string) ([]IssueRelationship, error)
	CreateIssueRelationship(context.Context, IssueRelationship) (IssueRelationship, error)
	DeleteIssueRelationship(context.Context, string, string, string) error
	ListRuns(context.Context, string) ([]Run, error)
	AssignIssue(context.Context, string, string, string) (Issue, Run, error)

	ListProviders(context.Context, *string) ([]Provider, error)
	GetProvider(context.Context, *string, string) (Provider, error)
	UpdateProvider(context.Context, *string, Provider) (Provider, error)

	ListModelProfiles(context.Context, *string) ([]ModelProfile, error)
	GetModelProfile(context.Context, *string, string) (ModelProfile, error)
	UpdateModelProfile(context.Context, *string, ModelProfile) (ModelProfile, error)

	ListAgents(context.Context, *string) ([]Agent, error)
	GetAgentInScope(context.Context, *string, string) (Agent, error)
	UpdateAgent(context.Context, *string, Agent) (Agent, error)
}
