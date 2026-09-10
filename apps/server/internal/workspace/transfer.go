package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

// TransferSnapshot captures the current authoritative Workspace as a Git bundle
// without touching its real Git index.
func (g *GitCLI) TransferSnapshot(ctx context.Context, repositoryPath, transferID string) ([]byte, error) {
	return sharedworkspace.SnapshotBundle(ctx, repositoryPath, transferID, g.binary, g.commandTimeout)
}

// ApplyTransferBundle restores the Runner's exact non-ignored Git state onto
// the authoritative Workspace: HEAD remains at the transfer Base, the Git
// index becomes the transfer Index, and checked-out files become Worktree.
func (g *GitCLI) ApplyTransferBundle(ctx context.Context, repositoryPath string, bundle []byte) error {
	if len(bundle) == 0 {
		return nil
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
	if _, err := g.run(ctx, "-C", repositoryPath, "bundle", "verify", name); err != nil {
		return fmt.Errorf("verify sync bundle: %w", err)
	}

	syncRef := "refs/agent-board/sync/" + TransferChecksum(bundle)[:32]
	if _, err := g.run(ctx, "-C", repositoryPath, "fetch", "--no-tags", "--no-write-fetch-head", name, "refs/heads/main:"+syncRef); err != nil {
		if _, fallbackErr := g.run(ctx, "-C", repositoryPath, "fetch", "--no-tags", "--no-write-fetch-head", name, "HEAD:"+syncRef); fallbackErr != nil {
			return fmt.Errorf("fetch sync bundle: %w", err)
		}
	}
	defer g.deleteTransferRef(repositoryPath, syncRef)

	state, err := sharedworkspace.ResolveSnapshotState(ctx, repositoryPath, syncRef, g.binary, g.commandTimeout)
	if err != nil {
		return fmt.Errorf("resolve runner workspace state: %w", err)
	}
	authoritativeHead, err := g.run(ctx, "-C", repositoryPath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("resolve authoritative workspace HEAD: %w", err)
	}
	if authoritativeHead != state.Base {
		return fmt.Errorf("runner workspace baseline does not match authoritative HEAD")
	}

	// First make the index and checked-out files exactly match the Runner's
	// worktree snapshot. This also handles deletions and paths that were
	// originally untracked but are represented as transport-only tree entries.
	if _, err := g.run(ctx, "-C", repositoryPath, "read-tree", "--reset", "-u", state.Worktree+"^{tree}"); err != nil {
		return fmt.Errorf("restore runner workspace files: %w", err)
	}
	// Then restore the Runner's real index without touching the files, leaving
	// staged and unstaged state exactly as it was on the Runner.
	if _, err := g.run(ctx, "-C", repositoryPath, "read-tree", state.Index+"^{tree}"); err != nil {
		return fmt.Errorf("restore runner workspace index: %w", err)
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

func TransferChecksum(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
