package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func validateIssuePlacement(input store.IssuePlacement) error {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.IssueID) == "" {
		return invalid("projectId and issueId are required")
	}
	if input.Status != nil && !store.ValidIssueStatus(*input.Status) {
		return invalid("invalid issue status")
	}
	if input.BeforeID != nil && *input.BeforeID == input.IssueID {
		return invalid("beforeId cannot reference the moved issue")
	}
	if input.AfterID != nil && *input.AfterID == input.IssueID {
		return invalid("afterId cannot reference the moved issue")
	}
	if input.BeforeID != nil && input.AfterID != nil && *input.BeforeID == *input.AfterID {
		return invalid("beforeId and afterId must reference different issues")
	}
	return nil
}

func (s *Service) PlaceIssue(ctx context.Context, input store.IssuePlacement) (store.Issue, error) {
	return s.placeIssue(ctx, input, store.EmptyObject)
}

func (s *Service) PlaceIssueWithActor(ctx context.Context, input store.IssuePlacement, actor json.RawMessage) (store.Issue, error) {
	return s.placeIssue(ctx, input, actor)
}

func (s *Service) placeIssue(ctx context.Context, input store.IssuePlacement, actor json.RawMessage) (store.Issue, error) {
	if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	if err := validateIssuePlacement(input); err != nil {
		return store.Issue{}, err
	}
	placementStore, ok := s.store.(store.IssuePlacementStore)
	if !ok {
		return store.Issue{}, NewError("issue_placement_unavailable", "issue placement is unavailable", store.ErrInvalidArgument)
	}
	result, err := placementStore.PlaceIssue(ctx, input, actor)
	if err != nil {
		return store.Issue{}, translateStoreError(err, "issue")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, result.Events)
	return result.Issue, nil
}

func (s *ProjectAccessService) PlaceIssue(ctx context.Context, actor AuthenticatedUser, input store.IssuePlacement) (store.Issue, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	encodedActor, err := json.Marshal(map[string]string{"type": store.ActorTypeHuman, "id": actor.ID})
	if err != nil {
		return store.Issue{}, err
	}
	return s.controlPlane.PlaceIssueWithActor(ctx, input, encodedActor)
}
