package store

import "context"

// Empty filters select all current Agent assignments during startup recovery.
// Configuration changes narrow the scan to the affected dependency.
type IssueExecutionFilter struct {
	AgentID        string
	ModelProfileID string
	ProviderID     string
}

type IssueExecutionStore interface {
	StartIssueRun(context.Context, string, string) (Run, Event, error)
	ReconcileIssueExecution(context.Context, IssueExecutionFilter) ([]Event, error)
}
