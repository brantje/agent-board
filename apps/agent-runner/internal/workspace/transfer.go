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

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
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
	// Git 2.53+ `bundle verify` requires an existing repository. `list-heads`
	// still rejects truncated/non-bundle payloads without that requirement.
	if _, err := runGit(ctx, "bundle", "list-heads", name); err != nil {
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

// SnapshotBundle captures the current session filesystem without mutating the
// session's real Git index. The same implementation is used by the server's
// authoritative Workspace snapshot path.
func SnapshotBundle(ctx context.Context, repositoryPath, transferID string) ([]byte, error) {
	return sharedworkspace.SnapshotBundle(ctx, repositoryPath, transferID, "git", commandTimeout)
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
