package runexec

import (
	"context"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type serverCheckoutFinalizer interface {
	FinalizeCheckout(context.Context, string, string) (string, error)
}

func (p *Processor) finalizeServerWorkspace(ctx context.Context, safe executioncontext.SafeContext) (string, error) {
	finalizer, ok := p.git.(serverCheckoutFinalizer)
	if !ok {
		return "", fmt.Errorf("workspace Git finalization is unavailable")
	}
	revisions, ok := p.store.(store.WorkspaceRevisionStore)
	if !ok {
		return "", fmt.Errorf("workspace revision store is unavailable")
	}
	startRevision, err := revisions.GetWorkspaceCurrentRevision(ctx, safe.Project.ID, safe.Workspace.ID)
	if err != nil {
		return "", err
	}
	startRevision = strings.TrimSpace(startRevision)
	if startRevision == "" && safe.Workspace.BaseRevision != nil {
		startRevision = strings.TrimSpace(*safe.Workspace.BaseRevision)
	}
	if startRevision == "" {
		return "", fmt.Errorf("execution start revision is unavailable")
	}
	revision, err := finalizer.FinalizeCheckout(ctx, safe.Workspace.Path, startRevision)
	if err != nil {
		return "", err
	}
	persisted, err := revisions.UpdateWorkspaceCurrentRevision(ctx, safe.Project.ID, safe.Workspace.ID, revision)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(persisted) != strings.TrimSpace(revision) {
		return "", fmt.Errorf("persisted Workspace revision does not match finalized Issue branch")
	}
	return revision, nil
}
