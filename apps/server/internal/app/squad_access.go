package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *ProjectAccessService) ListSquads(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.Squad, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.controlPlane.ListSquads(ctx, projectID)
}

func (s *ProjectAccessService) GetSquad(ctx context.Context, actor AuthenticatedUser, projectID, squadID string) (store.Squad, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return store.Squad{}, err
	}
	return s.controlPlane.GetSquad(ctx, projectID, squadID)
}

func (s *ProjectAccessService) CreateSquad(ctx context.Context, actor AuthenticatedUser, input store.Squad) (store.Squad, error) {
	if err := s.AuthorizeAdministration(ctx, actor, input.ProjectID); err != nil {
		return store.Squad{}, err
	}
	return s.controlPlane.CreateSquad(ctx, input)
}

func (s *ProjectAccessService) UpdateSquad(ctx context.Context, actor AuthenticatedUser, input store.Squad) (store.Squad, error) {
	if err := s.AuthorizeAdministration(ctx, actor, input.ProjectID); err != nil {
		return store.Squad{}, err
	}
	return s.controlPlane.UpdateSquad(ctx, input)
}

func (s *ProjectAccessService) DeleteSquad(ctx context.Context, actor AuthenticatedUser, projectID, squadID string) error {
	if err := s.AuthorizeAdministration(ctx, actor, projectID); err != nil {
		return err
	}
	return s.controlPlane.DeleteSquad(ctx, projectID, squadID)
}
