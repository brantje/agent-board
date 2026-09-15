package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func validateIssuePatch(patch store.IssuePatch) error {
	if patch.Title != nil && strings.TrimSpace(*patch.Title) == "" {
		return invalid("issue title is required")
	}
	if patch.Status != nil {
		switch *patch.Status {
		case "BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE":
		default:
			return invalid("invalid issue status")
		}
	}
	if patch.Priority != nil && (*patch.Priority < 0 || *patch.Priority > 4) {
		return invalid("issue priority must be between 0 and 4")
	}
	return nil
}

func (s *Service) PatchIssue(ctx context.Context, patch store.IssuePatch) (store.Issue, error) {
	return s.patchIssue(ctx, patch, store.EmptyObject)
}

func (s *Service) PatchIssueWithActor(ctx context.Context, patch store.IssuePatch, actor json.RawMessage) (store.Issue, error) {
	return s.patchIssue(ctx, patch, actor)
}

func (s *Service) patchIssue(ctx context.Context, patch store.IssuePatch, actor json.RawMessage) (store.Issue, error) {
	if _, err := s.GetProject(ctx, patch.ProjectID); err != nil {
		return store.Issue{}, err
	}
	if err := validateIssuePatch(patch); err != nil {
		return store.Issue{}, err
	}
	mutationStore, ok := s.store.(store.IssuePatchMutationStore)
	if !ok {
		return store.Issue{}, NewError("issue_mutation_unavailable", "partial issue mutation is unavailable", store.ErrInvalidArgument)
	}
	result, err := mutationStore.UpdateIssuePatchMutation(ctx, patch, actor)
	if err != nil {
		return store.Issue{}, translateStoreError(err, "issue")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, result.Events)
	return result.Issue, nil
}

func (s *ProjectAccessService) PatchIssue(ctx context.Context, actor AuthenticatedUser, patch store.IssuePatch) (store.Issue, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, patch.ProjectID); err != nil {
		return store.Issue{}, err
	}
	encodedActor, err := json.Marshal(map[string]string{"type": store.ActorTypeHuman, "id": actor.ID})
	if err != nil {
		return store.Issue{}, err
	}
	return s.controlPlane.PatchIssueWithActor(ctx, patch, encodedActor)
}
