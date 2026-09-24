package store

import "time"

const (
	AgentWorkRequestAuthorityIssue     = "ISSUE"
	AgentWorkRequestAuthorityParentRun = "PARENT_RUN"
)

// AgentWorkRequest is the durable admission/coalescing identity for comment-
// triggered Agent work. Run and scheduler lifecycle state remains authoritative
// in the existing execution model and is referenced through Delegation.
type AgentWorkRequest struct {
	ID            string
	ProjectID     string
	IssueID       string
	WorkspaceID   string
	TargetAgentID string
	AuthorityKind string
	ParentRunID   *string
	DelegationID  *string
	SealedAt      *time.Time
	ClosedAt      *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SameAgentWorkRequestCompatibility defines the smallest current compatibility
// boundary for comment-triggered work. Parent-Run authority never coalesces
// across authoritative parents or with Issue-origin work.
func SameAgentWorkRequestCompatibility(left, right AgentWorkRequest) bool {
	if left.ProjectID != right.ProjectID ||
		left.IssueID != right.IssueID ||
		left.WorkspaceID != right.WorkspaceID ||
		left.TargetAgentID != right.TargetAgentID ||
		left.AuthorityKind != right.AuthorityKind {
		return false
	}
	switch left.AuthorityKind {
	case AgentWorkRequestAuthorityIssue:
		return left.ParentRunID == nil && right.ParentRunID == nil
	case AgentWorkRequestAuthorityParentRun:
		return left.ParentRunID != nil && right.ParentRunID != nil && *left.ParentRunID == *right.ParentRunID
	default:
		return false
	}
}
