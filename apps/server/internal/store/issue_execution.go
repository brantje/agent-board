package store

import "context"

// Empty filters select all current Agent assignments during startup recovery.
// Configuration changes narrow the scan to the affected dependency.
type IssueExecutionFilter struct {
	ProjectID      string
	AgentID        string
	ModelProfileID string
	ProviderID     string
}

type IssueExecutionScope struct {
	ProjectID string
	AgentID   string
}

type IssueExecutionStore interface {
	StartIssueRun(context.Context, string, string) (Run, Event, error)
	ReconcileIssueExecution(context.Context, IssueExecutionFilter) ([]Event, error)
}

// EngineRegistrationStore binds execution-configuration validation to the
// Engine adapters registered by the application. It is configuration only;
// Runner availability remains scheduler admission policy.
type EngineRegistrationStore interface {
	SetEngineRegistered(func(string) bool)
}

// IssueExecutionReadinessStore exposes the same Agent execution-validity policy
// used by Run creation so configuration recovery can detect false -> true
// transitions without maintaining a second readiness matrix.
type IssueExecutionReadinessStore interface {
	RunnableIssueExecutionScopes(context.Context, IssueExecutionFilter) ([]IssueExecutionScope, error)
}

const (
	IssueExecutionBacklog                  = "BACKLOG"
	IssueExecutionNotAgentOwned            = "NOT_AGENT_OWNED"
	IssueExecutionConfigurationUnavailable = "CONFIGURATION_UNAVAILABLE"
	IssueExecutionActive                   = "ACTIVE"
	IssueExecutionReady                    = "READY"
)

type IssueExecutionState struct {
	State     string
	CanStart  bool
	ActiveRun *Run
}

type IssueExecutionStateStore interface {
	GetIssueExecutionState(context.Context, string, string) (IssueExecutionState, error)
}
