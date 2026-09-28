package app

import (
	"context"
	"errors"
	"strings"

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

func sourceConnectionOwnedByScope(value store.SourceConnection, scope *string) bool {
	if scope == nil || value.ProjectID == nil {
		return scope == nil && value.ProjectID == nil
	}
	return *scope == *value.ProjectID
}

func (s *Service) GetSourceConnectionForMutation(ctx context.Context, scope *string, id string) (store.SourceConnection, error) {
	value, err := s.GetSourceConnection(ctx, scope, id)
	if err != nil {
		return store.SourceConnection{}, err
	}
	if !sourceConnectionOwnedByScope(value, scope) {
		return store.SourceConnection{}, translateStoreError(store.ErrNotFound, "source_connection")
	}
	return value, nil
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
	current, err := s.GetSourceConnectionForMutation(ctx, scope, input.ID)
	if err != nil {
		return store.SourceConnection{}, err
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


func (s *Service) prepareConnectedProjectSource(ctx context.Context, input store.Project) (store.Project, error) {
	connectionID := ""
	if input.SourceConnectionID != nil {
		connectionID = strings.TrimSpace(*input.SourceConnectionID)
	}
	repositoryID := ""
	if input.SourceRepositoryID != nil {
		repositoryID = strings.TrimSpace(*input.SourceRepositoryID)
	}
	if connectionID == "" || repositoryID == "" {
		return store.Project{}, invalid("connected Projects require sourceConnectionId and sourceRepositoryId")
	}

	var scope *string
	if projectID := strings.TrimSpace(input.ID); projectID != "" {
		scope = &projectID
	}
	connection, err := s.GetSourceConnection(ctx, scope, connectionID)
	if err != nil {
		return store.Project{}, err
	}
	repository, err := s.GetSourceRepository(ctx, scope, connection.ID, repositoryID)
	if err != nil {
		return store.Project{}, err
	}

	ref := ""
	if input.SourceRef != nil {
		ref = strings.TrimSpace(*input.SourceRef)
	}
	if ref == "" {
		ref = strings.TrimSpace(repository.DefaultBranch)
	}
	if ref == "" {
		return store.Project{}, invalid("sourceRef is required when the connected repository has no default branch")
	}

	input.SourceConnectionID = &connection.ID
	input.SourceRepositoryID = &repository.ID
	input.SourceRef = &ref
	input.CloneURL = nil
	input.RepositoryPath = ""
	input.DefaultBranch = ""
	return input, nil
}
