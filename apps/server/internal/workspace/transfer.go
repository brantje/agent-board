package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

// TransferSnapshot captures the current authoritative Workspace as a Git bundle
// without touching its real Git index.
func (g *GitCLI) TransferSnapshot(ctx context.Context, repositoryPath, transferID string) ([]byte, error) {
	return sharedworkspace.SnapshotBundle(ctx, repositoryPath, transferID, g.binary, g.commandTimeout)
}

// ApplyTransferBundle applies only the filesystem delta produced by the Runner.
// The authoritative HEAD and Git index are intentionally untouched: staging is
// server-owned state, while Runner synchronization is an opaque Workspace
// transport detail. Pre-existing dirty/untracked state is included in the
// current worktree snapshot so it is not accidentally re-applied.
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

	currentWorktree, err := g.snapshotWorktreeTree(ctx, repositoryPath)
	if err != nil {
		return err
	}
	patch, err := g.runTransferBytes(ctx, nil, nil,
		"-C", repositoryPath,
		"diff", "--binary", "--full-index", "--no-ext-diff", "--no-renames",
		currentWorktree, state.Worktree+"^{tree}", "--",
	)
	if err != nil {
		return fmt.Errorf("diff runner workspace state: %w", err)
	}
	if len(patch) == 0 {
		return nil
	}
	if _, err := g.runTransferBytes(ctx, nil, patch, "-C", repositoryPath, "apply", "--check", "--whitespace=nowarn", "-"); err != nil {
		return fmt.Errorf("check runner workspace changes: %w", err)
	}
	if _, err := g.runTransferBytes(ctx, nil, patch, "-C", repositoryPath, "apply", "--whitespace=nowarn", "-"); err != nil {
		return fmt.Errorf("apply runner workspace changes: %w", err)
	}
	return nil
}

// snapshotWorktreeTree captures the authoritative non-ignored filesystem using
// an isolated temporary index. It never reads from or writes to .git/index.
func (g *GitCLI) snapshotWorktreeTree(ctx context.Context, repositoryPath string) (string, error) {
	tempRoot, err := os.MkdirTemp("", ".agent-board-apply-index-*")
	if err != nil {
		return "", fmt.Errorf("create apply snapshot directory: %w", err)
	}
	defer os.RemoveAll(tempRoot)
	indexEnv := []string{"GIT_INDEX_FILE=" + tempRoot + string(os.PathSeparator) + "index"}
	if _, err := g.runTransferBytes(ctx, indexEnv, nil, "-C", repositoryPath, "read-tree", "HEAD^{tree}"); err != nil {
		return "", fmt.Errorf("initialize apply snapshot index: %w", err)
	}
	if _, err := g.runTransferBytes(ctx, indexEnv, nil, "-C", repositoryPath, "add", "-A", "--", "."); err != nil {
		return "", fmt.Errorf("capture authoritative worktree: %w", err)
	}
	tree, err := g.runTransferBytes(ctx, indexEnv, nil, "-C", repositoryPath, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write authoritative worktree tree: %w", err)
	}
	resolved := strings.TrimSpace(string(tree))
	if resolved == "" {
		return "", fmt.Errorf("authoritative worktree tree is empty")
	}
	return resolved, nil
}

func (g *GitCLI) runTransferBytes(ctx context.Context, extraEnv []string, stdin []byte, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, g.commandTimeout)
	defer cancel()
	commandArgs := append(hardenedGitConfig(), args...)
	cmd := exec.CommandContext(commandCtx, g.binary, commandArgs...)
	cmd.Env = append(hardenedGitEnv(), extraEnv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if contextErr := commandCtx.Err(); contextErr != nil {
			return nil, fmt.Errorf("git %s: %w", commandName(args), contextErr)
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
	return append([]byte(nil), stdout.Bytes()...), nil
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
