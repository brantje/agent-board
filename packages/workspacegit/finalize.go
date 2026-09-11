package workspacegit

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FinalizeCheckoutOnBranch verifies that the checkout still belongs to the
// expected durable Issue branch before committing leftovers and validating
// execution-start ancestry.
func FinalizeCheckoutOnBranch(ctx context.Context, repositoryPath, expectedBranch, startRevision, gitBinary string, commandTimeout time.Duration) (string, error) {
	expectedBranch = strings.TrimSpace(expectedBranch)
	if expectedBranch == "" {
		return "", fmt.Errorf("expected Issue branch is required")
	}
	branch, err := CurrentBranch(ctx, repositoryPath, gitBinary, commandTimeout)
	if err != nil {
		return "", err
	}
	if branch != expectedBranch {
		return "", fmt.Errorf("workspace branch changed from %q to %q", expectedBranch, branch)
	}
	return FinalizeCheckout(ctx, repositoryPath, startRevision, gitBinary, commandTimeout)
}
