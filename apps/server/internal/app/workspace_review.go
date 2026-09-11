package app

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
	workspacepkg "github.com/brantje/agent-board/apps/server/internal/workspace"
)

type reviewedRevisionMaterializer interface {
	ApplyReviewedRevision(context.Context, store.Project, store.Review) (string, error)
}

type reviewedCandidateMaterializer interface {
	ApplyReviewedCandidate(context.Context, store.Project, string, workspacepkg.AcceptedCandidate) (string, error)
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

// ApplyReviewedCandidate is retained only while older callers are migrated off
// the pre-#72 snapshot delivery path.
func (s *WorkspaceService) ApplyReviewedCandidate(ctx context.Context, project store.Project, reviewID string, candidate workspacepkg.AcceptedCandidate) (string, error) {
	if s == nil || s.materializer == nil {
		return "", fmt.Errorf("workspace review delivery is unavailable")
	}
	applier, ok := s.materializer.(reviewedCandidateMaterializer)
	if !ok {
		return "", fmt.Errorf("workspace review delivery is unavailable")
	}
	revision, err := applier.ApplyReviewedCandidate(ctx, project, reviewID, candidate)
	if err != nil {
		return "", NewError("review_apply_failed", "accepted candidate could not be applied to the current Project Workspace", err)
	}
	return revision, nil
}
