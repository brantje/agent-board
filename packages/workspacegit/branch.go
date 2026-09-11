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

// BranchBundle exports the currently checked-out branch exactly as Git
// history. The worktree must be clean: mutable index/worktree state is not a
// durable Agent Board execution boundary.
func BranchBundle(ctx context.Context, repositoryPath, transferID, gitBinary string, commandTimeout time.Duration) ([]byte, error) {
	transferID = strings.TrimSpace(transferID)
	if transferID == "" {
		return nil, fmt.Errorf("workspace transfer id is required")
	}
	binary, err := resolveGitBinary(gitBinary, commandTimeout)
	if err != nil {
		return nil, err
	}
	branch, err := CurrentBranch(ctx, repositoryPath, binary, commandTimeout)
	if err != nil {
		return nil, err
	}
	clean, err := IsClean(ctx, repositoryPath, binary, commandTimeout)
	if err != nil {
		return nil, err
	}
	if !clean {
		return nil, fmt.Errorf("workspace must be clean before branch transfer")
	}
	revision, err := HeadRevision(ctx, repositoryPath, binary, commandTimeout)
	if err != nil {
		return nil, err
	}

	tempRoot, err := os.MkdirTemp("", ".agent-board-branch-transfer-*")
	if err != nil {
		return nil, fmt.Errorf("create workspace transfer temp directory: %w", err)
	}
	defer os.RemoveAll(tempRoot)

	transferRef := "refs/agent-board/transfer/" + safeTransferComponent(transferID)
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "update-ref", transferRef, revision); err != nil {
		return nil, fmt.Errorf("pin workspace transfer ref: %w", err)
	}
	defer deleteRef(repositoryPath, transferRef, binary, commandTimeout)

	privateRepo := filepath.Join(tempRoot, "bundle.git")
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "init", "--bare", "-q", privateRepo); err != nil {
		return nil, fmt.Errorf("initialize private transfer repository: %w", err)
	}
	branchRef := "refs/heads/" + branch
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil,
		"--git-dir", privateRepo, "fetch", "--no-tags", "--no-write-fetch-head", repositoryPath,
		transferRef+":"+branchRef,
	); err != nil {
		return nil, fmt.Errorf("copy workspace transfer objects: %w", err)
	}
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "--git-dir", privateRepo, "symbolic-ref", "HEAD", branchRef); err != nil {
		return nil, fmt.Errorf("set workspace transfer bundle HEAD: %w", err)
	}
	bundlePath := filepath.Join(tempRoot, "workspace.bundle")
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "--git-dir", privateRepo, "bundle", "create", bundlePath, branchRef); err != nil {
		return nil, fmt.Errorf("create workspace transfer bundle: %w", err)
	}
	payload, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("read workspace transfer bundle: %w", err)
	}
	return payload, nil
}

// BundleHead returns the single branch and revision exported by an Agent Board
// branch bundle. Branch-only transfer intentionally rejects bundles with
// multiple heads so callers never have to guess which history is authoritative.
func BundleHead(ctx context.Context, bundlePath, gitBinary string, commandTimeout time.Duration) (string, string, error) {
	binary, err := resolveGitBinary(gitBinary, commandTimeout)
	if err != nil {
		return "", "", err
	}
	output, err := runGit(ctx, binary, commandTimeout, nil, nil, "bundle", "list-heads", bundlePath)
	if err != nil {
		return "", "", fmt.Errorf("inspect workspace transfer bundle: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 1 || strings.TrimSpace(lines[0]) == "" {
		return "", "", fmt.Errorf("workspace transfer bundle must contain exactly one branch")
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 2 || !strings.HasPrefix(fields[1], "refs/heads/") {
		return "", "", fmt.Errorf("workspace transfer bundle head is invalid")
	}
	branch := strings.TrimPrefix(fields[1], "refs/heads/")
	if strings.TrimSpace(branch) == "" {
		return "", "", fmt.Errorf("workspace transfer bundle branch is missing")
	}
	return branch, fields[0], nil
}

// FinalizeCheckout preserves agent-created commits and commits any remaining
// tracked/deleted/untracked non-ignored changes. The resulting history must
// still contain the exact execution start revision.
func FinalizeCheckout(ctx context.Context, repositoryPath, startRevision, gitBinary string, commandTimeout time.Duration) (string, error) {
	binary, err := resolveGitBinary(gitBinary, commandTimeout)
	if err != nil {
		return "", err
	}
	startRevision = strings.TrimSpace(startRevision)
	if startRevision == "" {
		return "", fmt.Errorf("execution start revision is required")
	}
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "rev-parse", "--verify", startRevision+"^{commit}"); err != nil {
		return "", fmt.Errorf("resolve execution start revision: %w", err)
	}
	conflicts, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return "", fmt.Errorf("inspect unresolved conflicts: %w", err)
	}
	if strings.TrimSpace(conflicts) != "" {
		return "", fmt.Errorf("workspace has unresolved Git conflicts")
	}
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "add", "-A", "--", "."); err != nil {
		return "", fmt.Errorf("stage workspace changes: %w", err)
	}
	changed, err := hasStagedChanges(ctx, repositoryPath, binary, commandTimeout)
	if err != nil {
		return "", err
	}
	if changed {
		if _, err := runGit(ctx, binary, commandTimeout, nil, nil,
			"-C", repositoryPath,
			"-c", "user.name=Agent Board",
			"-c", "user.email=agent-board@localhost",
			"commit", "-m", "Agent Board execution hand-back",
		); err != nil {
			return "", fmt.Errorf("commit workspace changes: %w", err)
		}
	}
	head, err := HeadRevision(ctx, repositoryPath, binary, commandTimeout)
	if err != nil {
		return "", err
	}
	if _, err := runGit(ctx, binary, commandTimeout, nil, nil, "-C", repositoryPath, "merge-base", "--is-ancestor", startRevision, head); err != nil {
		return "", fmt.Errorf("workspace history no longer contains execution start revision %s", startRevision)
	}
	clean, err := IsClean(ctx, repositoryPath, binary, commandTimeout)
	if err != nil {
		return "", err
	}
	if !clean {
		return "", fmt.Errorf("workspace is not clean after finalization")
	}
	return head, nil
}

func CurrentBranch(ctx context.Context, repositoryPath, gitBinary string, commandTimeout time.Duration) (string, error) {
	branch, err := runGit(ctx, gitBinary, commandTimeout, nil, nil, "-C", repositoryPath, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve workspace branch: %w", err)
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("workspace must have a checked-out branch")
	}
	return branch, nil
}

func HeadRevision(ctx context.Context, repositoryPath, gitBinary string, commandTimeout time.Duration) (string, error) {
	revision, err := runGit(ctx, gitBinary, commandTimeout, nil, nil, "-C", repositoryPath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve workspace HEAD: %w", err)
	}
	return strings.TrimSpace(revision), nil
}

func IsClean(ctx context.Context, repositoryPath, gitBinary string, commandTimeout time.Duration) (bool, error) {
	status, err := runGit(ctx, gitBinary, commandTimeout, nil, nil, "-C", repositoryPath, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return false, fmt.Errorf("inspect workspace status: %w", err)
	}
	return strings.TrimSpace(status) == "", nil
}

func hasStagedChanges(ctx context.Context, repositoryPath, binary string, commandTimeout time.Duration) (bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	args := append(hardenedGitConfig(), "-C", repositoryPath, "diff", "--cached", "--quiet", "--exit-code")
	cmd := exec.CommandContext(commandCtx, binary, args...)
	cmd.Env = hardenedGitEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if commandCtx.Err() != nil {
		return false, fmt.Errorf("git diff: %w", commandCtx.Err())
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return false, fmt.Errorf("inspect staged workspace changes: %w: %s", err, detail)
		}
		return false, fmt.Errorf("inspect staged workspace changes: %w", err)
	}
	return true, nil
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
