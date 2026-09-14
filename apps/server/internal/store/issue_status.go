package store

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrIssueStatusRecoverySuperseded = errors.New("store: issue status recovery superseded")

// ValidIssueStatus reports whether status is part of the v0.1 Issue board contract.
func ValidIssueStatus(status string) bool {
	switch status {
	case "BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE":
		return true
	default:
		return false
	}
}

type IssueStatusMutation struct {
	ProjectID   string
	IssueID     string
	Status      string
	Actor       json.RawMessage
	RunID       *string
	AgentID     *string
	WorkspaceID *string
	Recovery    bool
}

// IssueStatusMutationStore is the canonical status-only Issue mutation boundary.
// Implementations must persist the status change and its causal Event atomically.
type IssueStatusMutationStore interface {
	SetIssueStatus(context.Context, IssueStatusMutation) (IssueMutationResult, error)
}
