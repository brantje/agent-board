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

// SnapshotBundle captures the repository's complete non-ignored filesystem state
// without reading from or writing to the repository's authoritative Git index.
func SnapshotBundle(ctx context.Context, repositoryPath, transferID, gitBinary string, commandTimeout time.Duration) ([]byte, error) {
	transferID = strings.TrimSpace(transferID)
	if transferID == "" {
		return nil, fmt.Errorf("workspace transfer id is required")
	}
	if commandTimeout <= 0 {
		return nil, fmt.Errorf("git command timeout must be positive")
	}
	if strings.TrimSpace(gitBinary) == "" {
		gitBinary = "git"
	}
	binary, err := exec.LookPath(gitBinary)
	if err != nil {
		return nil, fmt.Errorf("find git executable: %w", err)
	}

	tempRoot, err := os.MkdirTemp("", ".agent-board-workspace-transfer-*")
	if err != nil {
		return nil, fmt.Errorf("create workspace transfer temp directory: %w", err)
	}
	defer os.RemoveAll(tempRoot)

	indexPath := filepath.Join(tempRoot, "index")
	indexEnv := []string{"GIT_INDEX_FILE=" + indexPath}
	head, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve workspace HEAD: %w", err)
	}
	if _, err := runGit(ctx, binary, commandTimeout, indexEnv, nil, "-C", repositoryPath, "read-tree", head); err != nil {
		return nil, fmt.Errorf("initialize workspace transfer index: %w", err)
	}
	if _, err := runGit(ctx, binary, commandTimeout, indexEnv, nil, "-C", repositoryPath, "add", "-A", "--", "."); err != nil {
		return nil, fmt.Errorf("snapshot workspace files: %w", err)
	}
	tree, err := runGit(ctx, binary, commandTimeout, indexEnv, nil, "-C", repositoryPath, "write-tree")
	if err != nil {
		return nil, fmt.Errorf("write workspace transfer tree: %w", err)
	}
	commit, err := runGit(ctx, binary, commandTimeout, nil, nil,
		"-C", repositoryPath,
		"-c", "user.name=Agent Board",
		"-c", "user.email=agent-board@localhost",
		"commit-tree", tree, "-p", head, "-m", "Agent Board workspace transfer",
	)
	if err != nil {
		return nil, fmt.Errorf("create workspace transfer commit: %w", err)
	}

	transferRef := "refs/agent-board/transfer/" + safeTransferComponent(transferID)
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "update-ref", transferRef, commit); err != nil {
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
