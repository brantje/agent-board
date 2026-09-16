package store

import (
	"context"
	"time"
)

const MaxDelegationTaskCharacters = 16 << 10

type Delegation struct {
	ID             string
	ProjectID      string
	IssueID        string
	ParentRunID    string
	ParentAgentID  string
	TargetAgentID  string
	Task           string
	DelegatedRunID string
	RequestKey     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type RequestDelegationCommand struct {
	ProjectID     string
	ParentRunID   string
	TargetAgentID string
	Task          string
	RequestKey    string
}

type RequestDelegationResult struct {
	Delegation   Delegation
	DelegatedRun Run
	SchedulerJob SchedulerJob
	Events       []Event
}

type DelegationStore interface {
	RequestDelegation(context.Context, RequestDelegationCommand) (RequestDelegationResult, error)
	GetDelegationByRun(context.Context, string, string) (Delegation, error)
	ListDelegationsByParentRun(context.Context, string, string) ([]Delegation, error)
}
