package httpapi

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (f *fakeControlPlaneStore) UpdateIssuePatchMutation(_ context.Context, patch store.IssuePatch, _ json.RawMessage) (store.IssueMutationResult, error) {
	issue, err := f.GetIssue(context.Background(), patch.ProjectID, patch.ID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if patch.Title != nil {
		issue.Title = *patch.Title
	}
	if patch.Description != nil {
		issue.Description = *patch.Description
	}
	if patch.Status != nil {
		issue.Status = *patch.Status
	}
	if patch.Priority != nil {
		issue.Priority = *patch.Priority
	}
	return store.IssueMutationResult{Issue: issue}, nil
}
