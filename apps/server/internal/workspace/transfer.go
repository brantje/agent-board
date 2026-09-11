package workspace

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

// TransferSnapshot exports the current clean Issue branch as normal Git
// history. Mutable staging/worktree state is intentionally not transported.
func (g *GitCLI) TransferSnapshot(ctx context.Context, repositoryPath, transferID string) ([]byte, error) {
	return sharedworkspace.BranchBundle(ctx, repositoryPath, transferID, g.binary, g.commandTimeout)
}

// ApplyTransferBundle imports a Runner-returned Issue branch by fast-forwarding
// the authoritative local Issue branch. Rewritten/diverged history is rejected.
func (g *GitCLI) ApplyTransferBundle(ctx context.Context, repositoryPath string, bundle []byte) error {
	if len(bundle) == 0 {
		return fmt.Errorf("workspace branch bundle is required")
	}
	clean, err := sharedworkspace.IsClean(ctx, repositoryPath, g.binary, g.commandTimeout)
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("authoritative Issue Workspace must be clean before branch import")
	}
	currentBranch, err := sharedworkspace.CurrentBranch(ctx, repositoryPath, g.binary, g.commandTimeout)
	if err != nil {
		return err
	}
	currentHead, err := sharedworkspace.HeadRevision(ctx, repositoryPath, g.binary, g.commandTimeout)
	if err != nil {
		return err
	}

	file, err := os.CreateTemp("", ".agent-board-sync-*.bundle")
	if err != nil {
		return fmt.Errorf("create sync bundle file: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure sync bundle file: %w", err)
	}
	if _, err := file.Write(bundle); err != nil {
		_ = file.Close()
		return fmt.Errorf("write sync bundle file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close sync bundle file: %w", err)
	}
	branch, advertisedHead, err := sharedworkspace.BundleHead(ctx, name, g.binary, g.commandTimeout)
	if err != nil {
		return err
	}
	if branch != currentBranch {
		return fmt.Errorf("runner returned branch %q, expected %q", branch, currentBranch)
	}

	syncRef := "refs/agent-board/sync/" + advertisedHead
	if _, err := g.run(ctx, "-C", repositoryPath, "fetch", "--no-tags", "--no-write-fetch-head", name, "refs/heads/"+branch+":"+syncRef); err != nil {
		return fmt.Errorf("fetch runner Issue branch: %w", err)
	}
	defer g.deleteTransferRef(repositoryPath, syncRef)
	fetchedHead, err := g.run(ctx, "-C", repositoryPath, "rev-parse", "--verify", syncRef+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve returned Issue branch: %w", err)
	}
	if strings.TrimSpace(fetchedHead) != advertisedHead {
		return fmt.Errorf("returned Issue branch head changed during import")
	}
	if _, err := g.run(ctx, "-C", repositoryPath, "merge-base", "--is-ancestor", currentHead, syncRef); err != nil {
		return fmt.Errorf("runner Issue branch no longer contains execution start revision %s", currentHead)
	}
	if _, err := g.run(ctx, "-C", repositoryPath, "merge", "--ff-only", syncRef); err != nil {
		return fmt.Errorf("fast-forward authoritative Issue branch: %w", err)
	}
	updatedHead, err := sharedworkspace.HeadRevision(ctx, repositoryPath, g.binary, g.commandTimeout)
	if err != nil {
		return err
	}
	if updatedHead != advertisedHead {
		return fmt.Errorf("authoritative Issue branch did not reach returned head")
	}
	clean, err = sharedworkspace.IsClean(ctx, repositoryPath, g.binary, g.commandTimeout)
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("authoritative Issue Workspace is not clean after branch import")
	}
	return nil
}

func (g *GitCLI) deleteTransferRef(repositoryPath, ref string) {
	timeout := g.commandTimeout
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, _ = g.run(ctx, "-C", repositoryPath, "update-ref", "-d", ref)
}
