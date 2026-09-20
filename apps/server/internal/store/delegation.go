package store

import (
	"context"
	"time"
)

const (
	MaxDelegationTaskCharacters           = 16 << 10
	MaxDelegationResultCharacters         = 4 << 10
	DelegationWorkspaceHandoffWaitReason  = "delegation_handoff"
	DelegationWorkspaceHandoffReadyReason = "delegation_handoff_ready"
	DelegationWorkspaceAccessWrite        = "WRITE"
	DelegationOutcomeSucceeded            = "SUCCEEDED"
	DelegationOutcomeFailed               = "FAILED"
	DelegationOutcomeCancelled            = "CANCELLED"
)

type Delegation struct {
	ID                       string
	ProjectID                string
	IssueID                  string
	ParentRunID              string
	ParentAgentID            string
	SourceCommentID          *string
	TargetAgentID            string
	Task                     string
	DelegatedRunID           string
	RequestKey               string
	Outcome                  *string
	ResultSummary            *string
	ResultEventID            *string
	WorkspaceChangesAccepted *bool
	ContinuationJobID        *string
	CompletedAt              *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type RequestDelegationCommand struct {
	ProjectID     string
	ParentRunID   string
	TargetAgentID string
	Task          string
	RequestKey    string
}

type RequestIssueDelegationCommand struct {
	ProjectID       string
	IssueID         string
	SourceCommentID string
	TargetAgentID   string
	Task            string
	RequestKey      string
}

type RequestDelegationResult struct {
	Delegation   Delegation
	DelegatedRun Run
	SchedulerJob SchedulerJob
	Events       []Event
}

type IssueDelegationStore interface {
	RequestIssueDelegation(context.Context, RequestIssueDelegationCommand) (RequestDelegationResult, error)
}

type DelegationStore interface {
	RequestDelegation(context.Context, RequestDelegationCommand) (RequestDelegationResult, error)
	GetDelegationByRun(context.Context, string, string) (Delegation, error)
	ListDelegationsByParentRun(context.Context, string, string) ([]Delegation, error)
}

// DelegationContinuationStore resolves the exact delegated result that created
// a parent RESUME job. Keeping this lookup keyed by scheduler job prevents a
// later delegation on the same parent Run from supplying stale continuation data.
type DelegationContinuationStore interface {
	GetDelegationByContinuationJob(context.Context, string, string, string) (Delegation, error)
}

// DelegationWorkspaceHandoffStore marks a delegated Run ready only after the
// authoritative parent execution has durably returned its current Workspace
// state. Scheduler transition then releases that Run atomically with the parent
// yielding Workspace ownership.
type DelegationWorkspaceHandoffStore interface {
	MarkDelegationWorkspaceHandoffReady(context.Context, string, string, string, string) error
}
