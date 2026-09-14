package store

import (
	"context"
	"encoding/json"
)

// ValidIssueStatus reports whether status is part of the v0.1 Issue board contract.
func ValidIssueStatus(status string) bool {
	switch status {
	case "BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE":
		return true
	default:
		return false
	}
}

// IssueStatusMutationStore is the canonical status-only Issue mutation boundary.
// Implementations must persist the status change and its causal Event atomically.
type IssueStatusMutationStore interface {
	SetIssueStatus(context.Context, string, string, string, json.RawMessage) (IssueMutationResult, error)
}
