package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Service) PlaceIssue(ctx context.Context, input store.IssueBoardPlacement) (store.Issue, error) {
	if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	board, ok := s.store.(store.IssueBoardStore)
	if !ok {
		return store.Issue{}, errors.New("issue board store is unavailable")
	}
	result, err := board.PlaceIssue(ctx, input)
	if err != nil {
		return store.Issue{}, translateStoreError(err, "issue")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, result.Events)
	return result.Issue, nil
}

func (s *ProjectAccessService) PlaceIssue(ctx context.Context, actor AuthenticatedUser, input store.IssueBoardPlacement) (store.Issue, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	encoded, err := json.Marshal(map[string]string{"type": store.ActorTypeHuman, "id": actor.ID})
	if err != nil {
		return store.Issue{}, err
	}
	input.Actor = encoded
	return s.controlPlane.PlaceIssue(ctx, input)
}
