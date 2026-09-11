package workspacegit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

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
