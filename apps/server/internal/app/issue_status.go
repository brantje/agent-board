package app

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Service) SetIssueStatus(ctx context.Context, projectID, issueID, status string, actor json.RawMessage) (store.Issue, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return store.Issue{}, err
	}
	if !store.ValidIssueStatus(status) {
		return store.Issue{}, invalid("invalid issue status")
	}
	mutationStore, ok := s.store.(store.IssueStatusMutationStore)
	if !ok {
		return store.Issue{}, NewError("issue_status_unavailable", "issue status mutation is unavailable", store.ErrInvalidArgument)
	}
	result, err := mutationStore.SetIssueStatus(ctx, store.IssueStatusMutation{
		ProjectID: projectID,
		IssueID:   issueID,
		Status:    status,
		Actor:     actor,
	})
	if err != nil {
		return store.Issue{}, translateStoreError(err, "issue")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, result.Events)
	return result.Issue, nil
}
