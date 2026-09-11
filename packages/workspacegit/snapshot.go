package workspacegit

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
)

const cleanupTimeout = 10 * time.Second

// SnapshotState is the three-state Git model encoded by an Agent Board
// Workspace transfer bundle. Worktree is the advertised bundle HEAD, Index is
// its first parent, and Base is the Index commit's first parent.
type SnapshotState struct {
	Base     string
	Index    string
	Worktree string
}

// SnapshotBundle captures HEAD, the real Git index, and the complete
// non-ignored working tree without mutating the repository's authoritative
// index or visible history. Two synthetic commits encode the transport-only
// state as Base -> Index -> Worktree; only Worktree is advertised as bundle
// HEAD.
func SnapshotBundle(ctx context.Context, repositoryPath, transferID, gitBinary string, commandTimeout time.Duration) ([]byte, error) {
	transferID = strings.TrimSpace(transferID)
	if transferID == "" {
		return nil, fmt.Errorf("workspace transfer id is required")
	}
	binary, err := resolveGitBinary(gitBinary, commandTimeout)
	if err != nil {
		return nil, err
	}

	tempRoot, err := os.MkdirTemp("", ".agent-board-workspace-transfer-*")
	if err != nil {
		return nil, fmt.Errorf("create workspace transfer temp directory: %w", err)
	}
	defer os.RemoveAll(tempRoot)

	head, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve workspace HEAD: %w", err)
	}
	indexTree, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "write-tree")
	if err != nil {
		return nil, fmt.Errorf("capture workspace index: %w", err)
	}
	indexCommit, err := runGit(ctx, binary, commandTimeout, nil, nil,
		"-C", repositoryPath,
		"-c", "user.name=Agent Board",
		"-c", "user.email=agent-board@localhost",
		"commit-tree", indexTree, "-p", head, "-m", "Agent Board workspace transfer index",
	)
	if err != nil {
		return nil, fmt.Errorf("create workspace transfer index commit: %w", err)
	}

	indexPath := filepath.Join(tempRoot, "index")
	indexEnv := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := runGit(ctx, binary, commandTimeout, indexEnv, nil, "-C", repositoryPath, "read-tree", indexTree); err != nil {
		return nil, fmt.Errorf("initialize workspace transfer index: %w", err)
	}
	if _, err := runGit(ctx, binary, commandTimeout, indexEnv, nil, "-C", repositoryPath, "add", "-A", "--", "."); err != nil {
		return nil, fmt.Errorf("snapshot workspace files: %w", err)
	}
	worktreeTree, err := runGit(ctx, binary, commandTimeout, indexEnv, nil, "-C", repositoryPath, "write-tree")
	if err != nil {
		return nil, fmt.Errorf("write workspace transfer tree: %w", err)
	}
	worktreeCommit, err := runGit(ctx, binary, commandTimeout, nil, nil,
		"-C", repositoryPath,
		"-c", "user.name=Agent Board",
		"-c", "user.email=agent-board@localhost",
		"commit-tree", worktreeTree, "-p", indexCommit, "-m", "Agent Board workspace transfer worktree",
	)
	if err != nil {
		return nil, fmt.Errorf("create workspace transfer worktree commit: %w", err)
	}

	transferRef := "refs/agent-board/transfer/" + safeTransferComponent(transferID)
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "update-ref", transferRef, worktreeCommit); err != nil {
		return nil, fmt.Errorf("pin workspace transfer ref: %w", err)
	}
	defer deleteRef(repositoryPath, transferRef, binary, commandTimeout)

	privateRepo := filepath.Join(tempRoot, "bundle.git")
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "init", "--bare", "-q", privateRepo); err != nil {
		return nil, fmt.Errorf("initialize private transfer repository: %w", err)
	}
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil,
		"--git-dir", privateRepo, "fetch", "--no-tags", "--no-write-fetch-head", repositoryPath,
		transferRef+":refs/heads/main",
	); err != nil {
		return nil, fmt.Errorf("copy workspace transfer objects: %w", err)
	}
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "--git-dir", privateRepo, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		return nil, fmt.Errorf("set workspace transfer bundle HEAD: %w", err)
	}
	bundlePath := filepath.Join(tempRoot, "workspace.bundle")
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "--git-dir", privateRepo, "bundle", "create", bundlePath, "HEAD"); err != nil {
		return nil, fmt.Errorf("create workspace transfer bundle: %w", err)
	}
	payload, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("read workspace transfer bundle: %w", err)
	}
	return payload, nil
}

// ResolveSnapshotState validates and resolves the Base -> Index -> Worktree
// commit chain from a fetched Agent Board transfer revision.
func ResolveSnapshotState(ctx context.Context, repositoryPath, revision, gitBinary string, commandTimeout time.Duration) (SnapshotState, error) {
	binary, err := resolveGitBinary(gitBinary, commandTimeout)
	if err != nil {
		return SnapshotState{}, err
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return SnapshotState{}, fmt.Errorf("workspace transfer revision is required")
	}
	worktree, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return SnapshotState{}, fmt.Errorf("resolve workspace transfer worktree: %w", err)
	}
	index, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "rev-parse", "--verify", worktree+"^1")
	if err != nil {
		return SnapshotState{}, fmt.Errorf("resolve workspace transfer index: %w", err)
	}
	base, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "rev-parse", "--verify", index+"^1")
	if err != nil {
		return SnapshotState{}, fmt.Errorf("resolve workspace transfer baseline: %w", err)
	}
	return SnapshotState{Base: base, Index: index, Worktree: worktree}, nil
}

// RestoreCheckoutState converts a freshly cloned transfer bundle from its
// transport-only Worktree commit into the original repository shape: HEAD at
// Base, the Git index at Index, and the checked-out files left at Worktree.
func RestoreCheckoutState(ctx context.Context, repositoryPath, revision, gitBinary string, commandTimeout time.Duration) (SnapshotState, error) {
	state, err := ResolveSnapshotState(ctx, repositoryPath, revision, gitBinary, commandTimeout)
	if err != nil {
		return SnapshotState{}, err
	}
	binary, err := resolveGitBinary(gitBinary, commandTimeout)
	if err != nil {
		return SnapshotState{}, err
	}
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "update-ref", "HEAD", state.Base); err != nil {
		return SnapshotState{}, fmt.Errorf("restore workspace transfer HEAD: %w", err)
	}
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "read-tree", state.Index+"^{tree}"); err != nil {
		return SnapshotState{}, fmt.Errorf("restore workspace transfer index: %w", err)
	}
	return state, nil
}

func resolveGitBinary(gitBinary string, commandTimeout time.Duration) (string, error) {
	if commandTimeout <= 0 {
		return "", fmt.Errorf("git command timeout must be positive")
	}
	if strings.TrimSpace(gitBinary) == "" {
		gitBinary = "git"
	}
	binary, err := exec.LookPath(gitBinary)
	if err != nil {
		return "", fmt.Errorf("find git executable: %w", err)
	}
	return binary, nil
}

func safeTransferComponent(transferID string) string {
	sum := sha256.Sum256([]byte(transferID))
	return hex.EncodeToString(sum[:])
}

func deleteRef(repositoryPath, ref, binary string, commandTimeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), minDuration(commandTimeout, cleanupTimeout))
	defer cancel()
	_, _ = runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "update-ref", "-d", ref)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func runGit(ctx context.Context, binary string, commandTimeout time.Duration, extraEnv []string, stdin []byte, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	commandArgs := append(hardenedGitConfig(), args...)
	cmd := exec.CommandContext(commandCtx, binary, commandArgs...)
	cmd.Env = append(hardenedGitEnv(), extraEnv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if commandCtx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", commandName(args), commandCtx.Err())
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			return "", fmt.Errorf("git %s: %w", commandName(args), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", commandName(args), err, message)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func commandName(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-C" || args[i] == "--git-dir" {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "-") {
			continue
		}
		return args[i]
	}
	return "command"
}

func hardenedGitConfig() []string {
	return []string{
		"-c", "safe.directory=*",
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "credential.helper=",
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.file.allow=user",
	}
}

func hardenedGitEnv() []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "SSH_") || upper == "HOME" || upper == "PAGER" || upper == "XDG_CONFIG_HOME" {
			continue
		}
		env = append(env, item)
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GIT_SSH_COMMAND=false",
		"GIT_PAGER=cat",
		"PAGER=cat",
	)
}
