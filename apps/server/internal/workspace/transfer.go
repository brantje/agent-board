package workspace

import (
	"bytes"
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

// TransferSnapshot captures the current authoritative Workspace as a Git bundle
// without touching its real Git index.
func (g *GitCLI) TransferSnapshot(ctx context.Context, repositoryPath, transferID string) ([]byte, error) {
	return sharedworkspace.SnapshotBundle(ctx, repositoryPath, transferID, g.binary, g.commandTimeout)
}

// ApplyTransferBundle applies only the Runner's filesystem delta to the
// authoritative working tree. HEAD and the authoritative Git index are never
// advanced by transport.
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

	remote, err := g.run(ctx, "-C", repositoryPath, "rev-parse", "--verify", syncRef+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve synced commit: %w", err)
	}
	base, err := g.run(ctx, "-C", repositoryPath, "rev-parse", "--verify", remote+"^")
	if err != nil {
		return fmt.Errorf("resolve synced baseline: %w", err)
	}
	patch, err := g.runBytes(ctx, nil, "-C", repositoryPath, "diff", "--binary", "--full-index", "--no-ext-diff", base, remote, "--")
	if err != nil {
		return fmt.Errorf("build runner workspace delta: %w", err)
	}
	if len(patch) == 0 {
		return nil
	}
	if _, err := g.runBytes(ctx, patch, "-C", repositoryPath, "apply", "--check", "--whitespace=nowarn", "-"); err != nil {
		if _, reverseErr := g.runBytes(ctx, patch, "-C", repositoryPath, "apply", "--reverse", "--check", "--whitespace=nowarn", "-"); reverseErr == nil {
			return nil
		}
		return fmt.Errorf("validate runner workspace delta: %w", err)
	}
	if _, err := g.runBytes(ctx, patch, "-C", repositoryPath, "apply", "--whitespace=nowarn", "-"); err != nil {
		return fmt.Errorf("apply runner workspace delta: %w", err)
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

func (g *GitCLI) runBytes(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, g.commandTimeout)
	defer cancel()
	commandArgs := append(hardenedGitConfig(), args...)
	cmd := exec.CommandContext(commandCtx, g.binary, commandArgs...)
	cmd.Env = hardenedGitEnv()
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if commandCtx.Err() != nil {
			return nil, fmt.Errorf("git %s: %w", commandName(args), commandCtx.Err())
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			return nil, fmt.Errorf("git %s: %w", commandName(args), err)
		}
		return nil, fmt.Errorf("git %s: %w: %s", commandName(args), err, message)
	}
	return stdout.Bytes(), nil
}

func TransferChecksum(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

var _ = filepath.Separator
