package postgres

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Store) GetReviewRevisions(ctx context.Context, projectID, reviewID string) (string, string, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(reviewID) == "" {
		return "", "", store.ErrInvalidArgument
	}
	var baseRevision, reviewRevision string
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(base_revision, ''), COALESCE(review_revision, '')
		FROM reviews
		WHERE project_id=$1 AND id=$2
	`, projectID, reviewID).Scan(&baseRevision, &reviewRevision); err != nil {
		return "", "", notFound(err)
	}
	return strings.TrimSpace(baseRevision), strings.TrimSpace(reviewRevision), nil
}

var _ store.ReviewRevisionStore = (*Store)(nil)
