package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

const commandTimeout = 5 * time.Minute

type CheckoutState struct {
	Branch        string
	StartRevision string
}

// MaterializeBranchBundle creates a session checkout from a branch-only Agent
// Board transfer and returns the exact branch/revision the execution starts at.
func MaterializeBranchBundle(ctx context.Context, repositoryPath string, bundle []byte) (CheckoutState, error) {
	if len(bundle) == 0 {
		return CheckoutState{}, fmt.Errorf("workspace branch bundle is required")
	}
	if err := os.RemoveAll(repositoryPath); err != nil {
		return CheckoutState{}, fmt.Errorf("reset session workspace: %w", err)
	}
	file, err := os.CreateTemp("", ".agent-board-transfer-*.bundle")
	if err != nil {
		return CheckoutState{}, fmt.Errorf("create transfer bundle file: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return CheckoutState{}, fmt.Errorf("secure transfer bundle file: %w", err)
	}
	if _, err := file.Write(bundle); err != nil {
		_ = file.Close()
		return CheckoutState{}, fmt.Errorf("write transfer bundle file: %w", err)
	}
	if err := file.Close(); err != nil {
		return CheckoutState{}, fmt.Errorf("close transfer bundle file: %w", err)
	}
	branch, revision, err := sharedworkspace.BundleHead(ctx, name, "git", commandTimeout)
	if err != nil {
		return CheckoutState{}, err
	}
	parent := filepath.Dir(repositoryPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return CheckoutState{}, fmt.Errorf("prepare session workspace parent: %w", err)
	}
	if _, err := runGit(ctx, "init", "-q", repositoryPath); err != nil {
		return CheckoutState{}, fmt.Errorf("initialize session repository: %w", err)
	}
	branchRef := "refs/heads/" + branch
	materializeRef := "refs/agent-board/materialize/" + revision
	if _, err := runGit(ctx, "-C", repositoryPath, "fetch", "--no-tags", "--no-write-fetch-head", name, branchRef+":"+materializeRef); err != nil {
		_ = os.RemoveAll(repositoryPath)
		return CheckoutState{}, fmt.Errorf("materialize transfer branch: %w", err)
	}
	if _, err := runGit(ctx, "-C", repositoryPath, "checkout", "-q", "-B", branch, materializeRef); err != nil {
		_ = os.RemoveAll(repositoryPath)
		return CheckoutState{}, fmt.Errorf("checkout transferred branch: %w", err)
	}
	_, _ = runGit(context.Background(), "-C", repositoryPath, "update-ref", "-d", materializeRef)
	actualBranch, err := sharedworkspace.CurrentBranch(ctx, repositoryPath, "git", commandTimeout)
	if err != nil {
		_ = os.RemoveAll(repositoryPath)
		return CheckoutState{}, err
	}
	actualRevision, err := sharedworkspace.HeadRevision(ctx, repositoryPath, "git", commandTimeout)
	if err != nil {
		_ = os.RemoveAll(repositoryPath)
		return CheckoutState{}, err
	}
	if actualBranch != branch || actualRevision != revision {
		_ = os.RemoveAll(repositoryPath)
		return CheckoutState{}, fmt.Errorf("materialized workspace does not match transferred branch head")
	}
	return CheckoutState{Branch: branch, StartRevision: revision}, nil
}

// MaterializeBundle remains the simple materialization API used by tests and
// callers that do not need to retain execution-start identity.
func MaterializeBundle(ctx context.Context, repositoryPath string, bundle []byte) error {
	_, err := MaterializeBranchBundle(ctx, repositoryPath, bundle)
	return err
}

// SnapshotBundle exports only the checked-out branch history. Callers must
// finalize the checkout first so the execution boundary is clean Git state.
func SnapshotBundle(ctx context.Context, repositoryPath, transferID string) ([]byte, error) {
	return sharedworkspace.BranchBundle(ctx, repositoryPath, transferID, "git", commandTimeout)
}

func FinalizeCheckout(ctx context.Context, repositoryPath string, state CheckoutState) (string, error) {
	return sharedworkspace.FinalizeCheckoutOnBranch(ctx, repositoryPath, state.Branch, state.StartRevision, "git", commandTimeout)
}

func IsRepository(ctx context.Context, repositoryPath string) bool {
	_, err := runGit(ctx, "-C", repositoryPath, "rev-parse", "--is-inside-work-tree")
	return err == nil
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
