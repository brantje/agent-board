package workspace

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func (g *GitCLI) applyCandidatePatch(ctx context.Context, repositoryPath string, patch []byte, updateIndex bool) error {
	if len(patch) == 0 {
		return nil
	}
	file, err := os.CreateTemp("", ".agent-board-review-*.patch")
	if err != nil {
		return fmt.Errorf("create candidate patch: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(patch); err != nil {
		_ = file.Close()
		return fmt.Errorf("write candidate patch: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close candidate patch: %w", err)
	}
	args := []string{"-C", repositoryPath, "apply", "--binary", "--whitespace=nowarn"}
	if updateIndex {
		args = append(args, "--index")
	}
	args = append(args, "--", name)
	if _, err := g.run(ctx, args...); err != nil {
		return fmt.Errorf("apply candidate patch: %w", err)
	}
	return nil
}

func (g *GitCLI) resetAcceptedCheckout(ctx context.Context, repositoryPath string) error {
	if _, err := g.run(ctx, "-C", repositoryPath, "reset", "--hard", "HEAD"); err != nil {
		return err
	}
	_, err := g.run(ctx, "-C", repositoryPath, "clean", "-fd")
	return err
}

func (g *GitCLI) commitAcceptedCandidate(ctx context.Context, repositoryPath, reviewID string) (string, error) {
	if _, err := g.run(ctx, "-C", repositoryPath, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	message := "Accept Agent Board review " + reviewID
	trailer := "Agent-Board-Review: " + reviewID
	if _, err := g.run(ctx,
		"-C", repositoryPath,
		"-c", "user.name=Agent Board",
		"-c", "user.email=agent-board@localhost",
		"commit", "--allow-empty", "-m", message, "-m", trailer,
	); err != nil {
		return "", err
	}
	return g.HeadRevision(ctx, repositoryPath)
}

func (g *GitCLI) findAcceptedReview(ctx context.Context, repositoryPath, reviewID string) (string, bool, error) {
	needle := "Agent-Board-Review: " + reviewID
	output, err := g.run(ctx, "-C", repositoryPath, "log", "--fixed-strings", "--grep", needle, "--format=%H", "-n", "1")
	if err != nil {
		return "", false, err
	}
	commit := strings.TrimSpace(output)
	return commit, commit != "", nil
}
