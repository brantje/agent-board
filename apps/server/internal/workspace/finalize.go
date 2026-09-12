package workspace

import (
	"context"

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

// FinalizeCheckout preserves agent-created commits and commits any remaining
// non-ignored work before a real execution hand-back boundary, while requiring
// the checkout to remain on the durable Issue branch.
func (g *GitCLI) FinalizeCheckout(ctx context.Context, repositoryPath, expectedBranch, startRevision string) (string, error) {
	return sharedworkspace.FinalizeCheckoutOnBranch(ctx, repositoryPath, expectedBranch, startRevision, g.binary, g.commandTimeout)
}
