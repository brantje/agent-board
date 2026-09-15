package store

import (
	"context"
	"encoding/json"
)

// Assignee is the single current Issue owner. Name is resolved by the store.
type Assignee struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (i Issue) AssignedTo() *Assignee {
	if i.AssigneeType == nil || i.AssigneeID == nil {
		return nil
	}
	name := ""
	if i.AssigneeName != nil {
		name = *i.AssigneeName
	}
	return &Assignee{Type: *i.AssigneeType, ID: *i.AssigneeID, Name: name}
}

type IssueMutationResult struct {
	Issue  Issue
	Events []Event
}

// IssuePatch preserves which editable Issue fields were supplied by a partial
// mutation. Nil fields remain unchanged against the transactionally locked row.
type IssuePatch struct {
	ProjectID   string
	ID          string
	Title       *string
	Description *string
	Status      *string
	Priority    *int
}

// IssueMutationStore exposes the transaction-aware Issue mutation result when
// the durable store can persist the causal Issue Event with the state change.
type IssueMutationStore interface {
	CreateIssueMutation(context.Context, Issue) (IssueMutationResult, error)
	UpdateIssueMutation(context.Context, Issue) (IssueMutationResult, error)
}

// IssueMutationActorStore preserves the same transactional Issue mutation while
// attributing its durable Event to the authenticated actor that requested it.
type IssueMutationActorStore interface {
	UpdateIssueMutationWithActor(context.Context, Issue, json.RawMessage) (IssueMutationResult, error)
}

// IssuePatchMutationStore applies only supplied editable fields while the
// current Issue row is locked, so stale metadata snapshots cannot overwrite a
// concurrent explicit Board-status decision.
type IssuePatchMutationStore interface {
	UpdateIssuePatchMutation(context.Context, IssuePatch, json.RawMessage) (IssueMutationResult, error)
}

type IssueAssignmentStore interface {
	SetIssueAssignee(context.Context, string, string, *Assignee, json.RawMessage) (IssueMutationResult, error)
	ListIssueAssignees(context.Context, string) ([]Assignee, error)
}
