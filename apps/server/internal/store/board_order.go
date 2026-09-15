package store

import (
	"context"
	"encoding/json"
)

const (
	ProjectNewIssuePlacementTop    = "top"
	ProjectNewIssuePlacementBottom = "bottom"
)

// IssuePlacement expresses a board move using destination neighbors. Durable
// numeric positions remain an implementation detail of the store.
type IssuePlacement struct {
	ProjectID string
	IssueID   string
	Status    *string
	BeforeID  *string
	AfterID   *string
}

type IssuePlacementStore interface {
	PlaceIssue(context.Context, IssuePlacement, json.RawMessage) (IssueMutationResult, error)
}

func ProjectNewIssuePlacement(settings json.RawMessage) (string, error) {
	if len(settings) == 0 {
		return ProjectNewIssuePlacementBottom, nil
	}
	var value struct {
		NewIssuePlacement string `json:"newIssuePlacement"`
	}
	if err := json.Unmarshal(settings, &value); err != nil {
		return "", ErrInvalidArgument
	}
	switch value.NewIssuePlacement {
	case "", ProjectNewIssuePlacementBottom:
		return ProjectNewIssuePlacementBottom, nil
	case ProjectNewIssuePlacementTop:
		return ProjectNewIssuePlacementTop, nil
	default:
		return "", ErrInvalidArgument
	}
}
