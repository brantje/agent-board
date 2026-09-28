package app

import (
	"context"
	"errors"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Service) ListSourceConnections(ctx context.Context, scope *string) ([]store.SourceConnection, error) {
	if err := s.ensureSourceStoreAndScope(ctx, scope); err != nil {
		return nil, err
	}
	values, err := s.sources.ListSourceConnections(ctx, scope)
	return values, translateStoreError(err, "source_connection")
}

func (s *Service) ensureSourceStoreAndScope(ctx context.Context, scope *string) error {
	if s == nil || s.sources == nil {
		return NewError("source_configuration_unavailable", "source configuration is unavailable", errors.New("source store is not configured"))
	}
	return s.ensureScope(ctx, scope)
}

func (s *Service) GetSourceConnection(ctx context.Context, scope *string, id string) (store.SourceConnection, error) {
	if err := s.ensureSourceStoreAndScope(ctx, scope); err != nil {
		return store.SourceConnection{}, err
	}
	value, err := s.sources.GetSourceConnection(ctx, scope, id)
	return value, translateStoreError(err, "source_connection")
}

func (s *Service) CreateSourceConnection(ctx context.Context, input store.SourceConnection) (store.SourceConnection, error) {
	if err := s.ensureSourceStoreAndScope(ctx, input.ProjectID); err != nil {
		return store.SourceConnection{}, err
	}
	normalized, err := normalizeSourceConnection(input)
	if err != nil {
		return store.SourceConnection{}, err
	}
	value, err := s.sources.CreateSourceConnection(ctx, normalized)
	return value, translateStoreError(err, "source_connection")
}

func (s *Service) UpdateSourceConnection(ctx context.Context, scope *string, input store.SourceConnection) (store.SourceConnection, error) {
	if err := s.ensureSourceStoreAndScope(ctx, scope); err != nil {
		return store.SourceConnection{}, err
	}
	current, err := s.sources.GetSourceConnection(ctx, scope, input.ID)
	if err != nil {
		return store.SourceConnection{}, translateStoreError(err, "source_connection")
	}
	input.ProjectID = current.ProjectID
	if input.CredentialRef == nil {
		input.CredentialRef = current.CredentialRef
	}
	normalized, err := normalizeSourceConnection(input)
	if err != nil {
		return store.SourceConnection{}, err
	}
	value, err := s.sources.UpdateSourceConnection(ctx, scope, normalized)
	return value, translateStoreError(err, "source_connection")
}

func (s *Service) ListSourceRepositories(ctx context.Context, scope *string, connectionID string) ([]store.SourceRepository, error) {
	if _, err := s.GetSourceConnection(ctx, scope, connectionID); err != nil {
		return nil, err
	}
	values, err := s.sources.ListSourceRepositories(ctx, scope, connectionID)
	return values, translateStoreError(err, "source_repository")
}

func (s *Service) GetSourceRepository(ctx context.Context, scope *string, connectionID, repositoryID string) (store.SourceRepository, error) {
	if _, err := s.GetSourceConnection(ctx, scope, connectionID); err != nil {
		return store.SourceRepository{}, err
	}
	value, err := s.sources.GetSourceRepository(ctx, scope, connectionID, repositoryID)
	return value, translateStoreError(err, "source_repository")
}

func (s *Service) UpsertSourceRepository(ctx context.Context, scope *string, input store.SourceRepository) (store.SourceRepository, error) {
	if _, err := s.GetSourceConnection(ctx, scope, input.SourceConnectionID); err != nil {
		return store.SourceRepository{}, err
	}
	if err := validateSourceRepository(input); err != nil {
		return store.SourceRepository{}, err
	}
	value, err := s.sources.UpsertSourceRepository(ctx, input)
	return value, translateStoreError(err, "source_repository")
}
