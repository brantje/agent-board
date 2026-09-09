package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const commandTimeout = 5 * time.Minute

// MaterializeBundle unpacks a Git bundle into the session workspace directory.
func MaterializeBundle(ctx context.Context, repositoryPath string, bundle []byte) error {
	if len(bundle) == 0 {
		return os.MkdirAll(repositoryPath, 0o755)
	}
	if err := os.RemoveAll(repositoryPath); err != nil {
		return fmt.Errorf("reset session workspace: %w", err)
	}
	file, err := os.CreateTemp("", ".agent-board-transfer-*.bundle")
	if err != nil {
		return fmt.Errorf("create transfer bundle file: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(bundle); err != nil {
		_ = file.Close()
		return fmt.Errorf("write transfer bundle file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close transfer bundle file: %w", err)
	}
	if _, err := runGit(ctx, "bundle", "verify", name); err != nil {
		return fmt.Errorf("verify transfer bundle: %w", err)
	}
	parent := filepath.Dir(repositoryPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("prepare session workspace parent: %w", err)
	}
	if _, err := runGit(ctx, "-C", parent, "clone", name, filepath.Base(repositoryPath)); err != nil {
		return fmt.Errorf("materialize transfer bundle: %w", err)
	}
	return nil
}

// SnapshotBundle captures the current session workspace as a Git bundle.
func SnapshotBundle(ctx context.Context, repositoryPath, transferID string) ([]byte, error) {
	transferID = strings.TrimSpace(transferID)
	if transferID == "" {
		return nil, fmt.Errorf("workspace transfer id is required")
	}
	if _, err := runGit(ctx, "-C", repositoryPath, "add", "-A", "--", "."); err != nil {
		return nil, fmt.Errorf("stage session workspace for sync: %w", err)
	}
	head, err := runGit(ctx, "-C", repositoryPath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve session workspace HEAD: %w", err)
	}
	commit := head
	staged, err := runGit(ctx, "-C", repositoryPath, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("inspect staged session workspace changes: %w", err)
	}
	if strings.TrimSpace(staged) != "" {
		tree, err := runGit(ctx, "-C", repositoryPath, "write-tree")
		if err != nil {
			return nil, fmt.Errorf("write session workspace sync tree: %w", err)
		}
		commit, err = runGit(ctx, "-C", repositoryPath, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit-tree", tree, "-p", head, "-m", "Agent Board workspace sync")
		if err != nil {
			return nil, fmt.Errorf("create session workspace sync commit: %w", err)
		}
	}
	if _, err := runGit(ctx, "-C", repositoryPath, "reset"); err != nil {
		return nil, fmt.Errorf("restore session workspace index after sync snapshot: %w", err)
	}
	syncRef := "refs/agent-board/sync/head"
	defer func() { _, _ = runGit(ctx, "-C", repositoryPath, "update-ref", "-d", syncRef) }()
	if _, err := runGit(ctx, "-C", repositoryPath, "update-ref", syncRef, commit); err != nil {
		return nil, fmt.Errorf("pin session workspace sync commit: %w", err)
	}
	bundlePath := filepath.Join(os.TempDir(), ".agent-board-sync-"+transferID+".bundle")
	defer os.Remove(bundlePath)
	if _, err := runGit(ctx, "-C", repositoryPath, "bundle", "create", bundlePath, syncRef); err != nil {
		if strings.Contains(err.Error(), "Refusing to create empty bundle") {
			return nil, nil
		}
		return nil, fmt.Errorf("create session workspace sync bundle: %w", err)
	}
	payload, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("read session workspace sync bundle: %w", err)
	}
	return payload, nil
}

func IsRepository(ctx context.Context, repositoryPath string) bool {
	_, err := runGit(ctx, "-C", repositoryPath, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

func TransferChecksum(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func runGit(ctx context.Context, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
