package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const transferRefPrefix = "refs/agent-board/transfer/"

// TransferSnapshot captures the current authoritative Workspace as a Git bundle.
func (g *GitCLI) TransferSnapshot(ctx context.Context, repositoryPath, transferID string) ([]byte, error) {
	transferID = strings.TrimSpace(transferID)
	if transferID == "" {
		return nil, fmt.Errorf("workspace transfer id is required")
	}
	if _, err := g.run(ctx, "-C", repositoryPath, "add", "-A", "--", "."); err != nil {
		return nil, fmt.Errorf("stage workspace for transfer: %w", err)
	}
	head, err := g.run(ctx, "-C", repositoryPath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve workspace HEAD: %w", err)
	}
	commit := head
	staged, err := g.run(ctx, "-C", repositoryPath, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("inspect staged workspace changes: %w", err)
	}
	if strings.TrimSpace(staged) != "" {
		tree, err := g.run(ctx, "-C", repositoryPath, "write-tree")
		if err != nil {
			return nil, fmt.Errorf("write workspace transfer tree: %w", err)
		}
		commit, err = g.run(ctx, "-C", repositoryPath, "commit-tree", tree, "-p", head, "-m", "Agent Board workspace transfer")
		if err != nil {
			return nil, fmt.Errorf("create workspace transfer commit: %w", err)
		}
	}
	if _, err := g.run(ctx, "-C", repositoryPath, "reset"); err != nil {
		return nil, fmt.Errorf("restore workspace index after transfer snapshot: %w", err)
	}
	transferRef := transferRefPrefix + transferID
	if _, err := g.run(ctx, "-C", repositoryPath, "update-ref", transferRef, commit); err != nil {
		return nil, fmt.Errorf("pin workspace transfer ref: %w", err)
	}
	defer func() { _, _ = g.run(ctx, "-C", repositoryPath, "update-ref", "-d", transferRef) }()
	bundlePath := filepath.Join(os.TempDir(), ".agent-board-transfer-"+transferID+".bundle")
	defer os.Remove(bundlePath)
	if err := g.writeCloneableBundle(ctx, repositoryPath, commit, bundlePath); err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("read workspace transfer bundle: %w", err)
	}
	return payload, nil
}

func (g *GitCLI) writeCloneableBundle(ctx context.Context, repositoryPath, commit, bundlePath string) error {
	private := filepath.Join(os.TempDir(), ".agent-board-bundle-"+filepath.Base(bundlePath))
	defer os.RemoveAll(private)
	if _, err := g.run(ctx, "clone", "--bare", repositoryPath, private); err != nil {
		return fmt.Errorf("create workspace transfer bundle: %w", err)
	}
	if _, err := g.run(ctx, "--git-dir", private, "update-ref", "refs/heads/main", commit); err != nil {
		return fmt.Errorf("create workspace transfer bundle: %w", err)
	}
	if _, err := g.run(ctx, "--git-dir", private, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		return fmt.Errorf("create workspace transfer bundle: %w", err)
	}
	if _, err := g.run(ctx, "--git-dir", private, "bundle", "create", bundlePath, "HEAD", "main"); err != nil {
		return fmt.Errorf("create workspace transfer bundle: %w", err)
	}
	return nil
}

// ApplyTransferBundle applies a runner-returned bundle onto the authoritative Workspace.
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
	head, err := g.run(ctx, "-C", repositoryPath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("resolve workspace HEAD before sync: %w", err)
	}
	syncRef := "refs/agent-board/sync/head"
	if _, err := g.run(ctx, "-C", repositoryPath, "fetch", name, "HEAD:"+syncRef); err != nil {
		if _, fallbackErr := g.run(ctx, "-C", repositoryPath, "fetch", name, "main:"+syncRef); fallbackErr != nil {
			return fmt.Errorf("fetch sync bundle: %w", err)
		}
	}
	remote, err := g.run(ctx, "-C", repositoryPath, "rev-parse", "--verify", syncRef+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve synced commit: %w", err)
	}
	defer func() { _, _ = g.run(ctx, "-C", repositoryPath, "update-ref", "-d", syncRef) }()
	if remote == head {
		return nil
	}
	if _, err := g.run(ctx, "-C", repositoryPath, "read-tree", "-m", "-u", head, remote); err != nil {
		return fmt.Errorf("merge synced tree into workspace: %w", err)
	}
	if _, err := g.run(ctx, "-C", repositoryPath, "add", "-A", "--", "."); err != nil {
		return fmt.Errorf("stage synced workspace changes: %w", err)
	}
	if _, err := g.run(ctx, "-C", repositoryPath, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "Synchronize workspace from runner"); err != nil {
		return fmt.Errorf("commit synced workspace changes: %w", err)
	}
	return nil
}

func TransferChecksum(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
