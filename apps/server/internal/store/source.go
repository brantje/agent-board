package store

import "context"

// SourceStore persists provider-neutral Source Connection and repository identities.
type SourceStore interface {
	ListSourceConnections(context.Context, *string) ([]SourceConnection, error)
	GetSourceConnection(context.Context, *string, string) (SourceConnection, error)
	CreateSourceConnection(context.Context, SourceConnection) (SourceConnection, error)
	UpdateSourceConnection(context.Context, *string, SourceConnection) (SourceConnection, error)
	ListSourceRepositories(context.Context, *string, string) ([]SourceRepository, error)
	GetSourceRepository(context.Context, *string, string, string) (SourceRepository, error)
	UpsertSourceRepository(context.Context, SourceRepository) (SourceRepository, error)
}
