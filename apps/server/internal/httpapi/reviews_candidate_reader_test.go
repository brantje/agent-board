package httpapi

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/evidence"
)

func (s *httpReviewStore) Open(context.Context, string) (evidence.ReviewCandidateSnapshot, error) {
	return evidence.ReviewCandidateSnapshot{}, nil
}
