package store

import "context"

// Empty filters select all current executable Issue ownership during startup recovery.
// Configuration changes narrow the scan to the affected dependency; SquadID
// narrows reconciliation to one canonical Squad owner.
type IssueExecutionFilter struct {
	ProjectID      string
	AgentID        string
	SquadID        string
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

type IssueExecutionAgent struct {
	ID   string
	Name string
}

type IssueExecutionState struct {
	State          string
	CanStart       bool
	ExecutionAgent *IssueExecutionAgent
	ActiveRun      *Run
}

type IssueExecutionStateStore interface {
	GetIssueExecutionState(context.Context, string, string) (IssueExecutionState, error)
}
