package app

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reviewedRevisionMaterializer interface {
	ApplyReviewedRevision(context.Context, store.Project, store.Review) (string, error)
}

// ApplyReviewedRevision is the trusted application boundary used by Review
// approval. Runtime/Engine code never receives access to the accepted Project
// Workspace.
func (s *WorkspaceService) ApplyReviewedRevision(ctx context.Context, project store.Project, review store.Review) (string, error) {
	if s == nil || s.materializer == nil {
		return "", fmt.Errorf("workspace review delivery is unavailable")
	}
	applier, ok := s.materializer.(reviewedRevisionMaterializer)
	if !ok {
		return "", fmt.Errorf("workspace review delivery is unavailable")
	}
	revision, err := applier.ApplyReviewedRevision(ctx, project, review)
	if err != nil {
		return "", NewError("review_apply_failed", "reviewed Git revision could not be applied to the current Project Workspace", err)
	}
	return revision, nil
}
