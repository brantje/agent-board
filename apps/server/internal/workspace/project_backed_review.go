package workspace

import (
	"context"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

// ApplyReviewedCandidate delivers the immutable candidate selected by a Review
// into the Project's accepted checkout. ProjectBackedMaterializer deliberately
// owns this operation because it owns both the accepted Project source and the
// Issue Workspace materialization boundary.
func (m *ProjectBackedMaterializer) ApplyReviewedCandidate(ctx context.Context, project store.Project, reviewID string, candidate AcceptedCandidate) (string, error) {
	if m == nil || m.issue == nil || m.projects == nil {
		return "", fmt.Errorf("apply reviewed candidate: %w", ErrInvalidMetadata)
	}
	locks := ProjectWorkspaceLockStore(m.issue.store)
	git, ok := m.issue.git.(candidateGit)
	if !ok {
		return "", fmt.Errorf("apply reviewed candidate: trusted Git candidate capability is unavailable: %w", ErrInvalidMetadata)
	}
	applier, err := NewCandidateApplier(locks, m.projects, git)
	if err != nil {
		return "", err
	}
	return applier.Apply(ctx, project, reviewID, candidate)
}
