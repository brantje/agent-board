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
