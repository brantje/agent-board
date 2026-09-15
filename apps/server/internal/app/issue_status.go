package app

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// UpdateIssueWithActor preserves the normal transactional Issue mutation while
// attributing its durable Event to the authenticated caller.
func (s *Service) UpdateIssueWithActor(ctx context.Context, input store.Issue, actor json.RawMessage) (store.Issue, error) {
	if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	if err := validateIssue(input); err != nil {
		return store.Issue{}, err
	}
	mutationStore, ok := s.store.(store.IssueMutationActorStore)
	if !ok {
		return store.Issue{}, NewError("issue_mutation_unavailable", "actor-aware issue mutation is unavailable", store.ErrInvalidArgument)
	}
	result, err := mutationStore.UpdateIssueMutationWithActor(ctx, input, actor)
	if err != nil {
		return store.Issue{}, translateStoreError(err, "issue")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, result.Events)
	return result.Issue, nil
}
