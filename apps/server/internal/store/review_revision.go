package store

import "context"

type ReviewRevisionStore interface {
	GetReviewRevisions(context.Context, string, string) (baseRevision, reviewRevision string, err error)
}
