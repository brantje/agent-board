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

type IssueAssignmentStore interface {
	SetIssueAssignee(context.Context, string, string, *Assignee, json.RawMessage) (Issue, Event, error)
	ListIssueAssignees(context.Context, string) ([]Assignee, error)
}
