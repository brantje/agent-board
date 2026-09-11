package workspace

import (
	"context"

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

// FinalizeCheckout preserves agent-created commits and commits any remaining
// non-ignored work before a real execution hand-back boundary.
func (g *GitCLI) FinalizeCheckout(ctx context.Context, repositoryPath, startRevision string) (string, error) {
	return sharedworkspace.FinalizeCheckout(ctx, repositoryPath, startRevision, g.binary, g.commandTimeout)
}
