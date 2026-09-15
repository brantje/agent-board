package store

import (
	"context"
	"encoding/json"
)

type IssueBoardPlacement struct {
	ProjectID     string
	IssueID       string
	Status        string
	BeforeIssueID *string
	Actor         json.RawMessage
}

type IssueBoardStore interface {
	PlaceIssue(context.Context, IssueBoardPlacement) (IssueMutationResult, error)
}
