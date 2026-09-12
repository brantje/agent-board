package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

// ApplyReviewedRevision integrates the exact commit pinned by Review into the
// current local Project target. The Issue branch is only a source of Git
// objects; delivery never reconstructs reviewed code from filesystem patches.
func (m *ProjectBackedMaterializer) ApplyReviewedRevision(ctx context.Context, project store.Project, review store.Review) (revision string, err error) {
	if m == nil || m.issue == nil || m.projects == nil || project.SourceType != store.ProjectSourceLocal {
		return "", fmt.Errorf("apply reviewed revision: %w", ErrInvalidMetadata)
	}
	if strings.TrimSpace(review.ID) == "" || strings.TrimSpace(review.IssueID) == "" || strings.TrimSpace(review.BaseRevision) == "" || strings.TrimSpace(review.ReviewRevision) == "" {
		return "", fmt.Errorf("apply reviewed revision: Review Git identity is incomplete: %w", ErrInvalidMetadata)
	}
	accepted, err := m.projects.EnsureProjectWorkspace(ctx, project)
	if err != nil {
		return "", err
	}
	issueWorkspace, err := m.issue.store.GetWorkspaceByIssue(ctx, project.ID, review.IssueID)
	if err != nil {
		return "", fmt.Errorf("load reviewed Issue Workspace: %w", err)
	}
	if issueWorkspace.BootstrapStatus != "READY" || strings.TrimSpace(issueWorkspace.Path) == "" || strings.TrimSpace(issueWorkspace.WorkingBranch) == "" {
		return "", fmt.Errorf("reviewed Issue Workspace is unavailable: %w", ErrInvalidMetadata)
	}
	git, ok := m.issue.git.(*GitCLI)
	if !ok {
		return "", fmt.Errorf("apply reviewed revision: trusted Git integration capability is unavailable: %w", ErrInvalidMetadata)
	}

	lock, err := m.issue.store.AcquireWorkspaceBootstrapLock(ctx, "project:"+project.ID)
	if err != nil {
		return "", fmt.Errorf("acquire Project Workspace approval lock: %w", err)
	}
	defer func() {
		if releaseErr := lock.Release(); err == nil && releaseErr != nil && strings.TrimSpace(revision) == "" {
			err = fmt.Errorf("release Project Workspace approval lock: %w", releaseErr)
		}
	}()

	branch, err := sharedworkspace.CurrentBranch(ctx, accepted.Path, git.binary, git.commandTimeout)
	if err != nil {
		return "", err
	}
	if branch != strings.TrimSpace(accepted.BaseBranch) {
		return "", fmt.Errorf("Project Workspace is on branch %q, expected %q", branch, accepted.BaseBranch)
	}
	clean, err := sharedworkspace.IsClean(ctx, accepted.Path, git.binary, git.commandTimeout)
	if err != nil {
		return "", err
	}
	if !clean {
		return "", fmt.Errorf("Project Workspace must be clean before review delivery")
	}
	originalHead, err := sharedworkspace.HeadRevision(ctx, accepted.Path, git.binary, git.commandTimeout)
	if err != nil {
		return "", err
	}

	tempRef := "refs/agent-board/review/" + strings.TrimSpace(review.ReviewRevision)
	if _, err := git.run(ctx, "-C", accepted.Path, "fetch", "--no-tags", "--no-write-fetch-head", issueWorkspace.Path,
		"refs/heads/"+issueWorkspace.WorkingBranch+":"+tempRef); err != nil {
		return "", fmt.Errorf("fetch reviewed Issue branch: %w", err)
	}
	defer git.deleteTransferRef(accepted.Path, tempRef)

	if _, err := git.run(ctx, "-C", accepted.Path, "rev-parse", "--verify", review.ReviewRevision+"^{commit}"); err != nil {
		return "", fmt.Errorf("resolve pinned review revision: %w", err)
	}
	if _, err := git.run(ctx, "-C", accepted.Path, "merge-base", "--is-ancestor", review.BaseRevision, review.ReviewRevision); err != nil {
		return "", fmt.Errorf("review revision no longer contains its Issue base revision")
	}
	if _, err := git.run(ctx, "-C", accepted.Path, "merge-base", "--is-ancestor", review.ReviewRevision, tempRef); err != nil {
		return "", fmt.Errorf("pinned review revision is no longer contained by the Issue branch")
	}
	if _, err := git.run(ctx, "-C", accepted.Path, "merge-base", "--is-ancestor", review.ReviewRevision, originalHead); err == nil {
		return originalHead, nil
	}

	if _, err := git.run(ctx,
		"-C", accepted.Path,
		"-c", "user.name=Agent Board",
		"-c", "user.email=agent-board@localhost",
		"merge", "--no-edit", review.ReviewRevision,
	); err != nil {
		rollbackCtx := context.WithoutCancel(ctx)
		_, abortErr := git.run(rollbackCtx, "-C", accepted.Path, "merge", "--abort")
		if abortErr != nil {
			_, resetErr := git.run(rollbackCtx, "-C", accepted.Path, "reset", "--hard", originalHead)
			abortErr = errors.Join(abortErr, resetErr)
		}
		return "", errors.Join(fmt.Errorf("integrate reviewed revision: %w", err), abortErr)
	}

	updatedHead, err := sharedworkspace.HeadRevision(ctx, accepted.Path, git.binary, git.commandTimeout)
	if err != nil {
		return "", err
	}
	clean, err = sharedworkspace.IsClean(ctx, accepted.Path, git.binary, git.commandTimeout)
	if err != nil {
		return "", err
	}
	if !clean {
		return "", fmt.Errorf("Project Workspace is not clean after review delivery")
	}
	return updatedHead, nil
}
