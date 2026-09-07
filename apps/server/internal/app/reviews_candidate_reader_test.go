package app

import (
	"context"
	"io"
	"strings"

	evidencepkg "github.com/brantje/agent-board/apps/server/internal/evidence"
)

func (s *reviewServiceStore) Open(context.Context, string) (evidencepkg.ReviewCandidateSnapshot, error) {
	if len(s.artifacts) == 0 {
		return evidencepkg.ReviewCandidateSnapshot{}, evidencepkg.ErrReviewCandidateNotFound
	}

	snapshot := evidencepkg.ReviewCandidateSnapshot{}
	if len(s.artifacts) > 1 {
		snapshot.StagedPatch = reviewCandidateTestSource("patch")
	}
	if len(s.artifacts) > 2 {
		snapshot.Files = []evidencepkg.ReviewCandidateFile{{
			Path:   "new.txt",
			Source: reviewCandidateTestSource("content"),
		}}
	}
	return snapshot, nil
}

func reviewCandidateTestSource(value string) evidencepkg.ReviewCandidateBlobSource {
	return func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(value)), nil
	}
}
