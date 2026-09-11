package postgres

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestPersistedReviewRevisionsReproduceGitDiff(t *testing.T) {
	repo := t.TempDir()
	reviewGit(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reviewGit(t, repo, "add", "README.md")
	reviewGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	baseRevision := reviewGitOutput(t, repo, "rev-parse", "HEAD")
	reviewGit(t, repo, "checkout", "-qb", "agent-board/AB-90")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("reviewed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "review.txt"), []byte("review evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reviewGit(t, repo, "add", "-A")
	reviewGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "reviewed")
	reviewRevision := reviewGitOutput(t, repo, "rev-parse", "HEAD")
	expectedDiff := reviewGitOutput(t, repo, "diff", baseRevision+".."+reviewRevision)
	if expectedDiff == "" {
		t.Fatal("test setup produced an empty Review diff")
	}

	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "review-git-identity")
	created, err := s.CreateReview(ctx, store.Review{
		ProjectID: fixture.project.ID,
		IssueID: fixture.issue.ID,
		RunID: fixture.run.ID,
		BaseRevision: baseRevision,
		ReviewRevision: reviewRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := s.GetReview(ctx, fixture.project.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.BaseRevision != baseRevision || persisted.ReviewRevision != reviewRevision {
		t.Fatalf("persisted Review revisions base=%q review=%q", persisted.BaseRevision, persisted.ReviewRevision)
	}
	actualDiff := reviewGitOutput(t, repo, "diff", persisted.BaseRevision+".."+persisted.ReviewRevision)
	if actualDiff != expectedDiff {
		t.Fatalf("persisted Review diff differs\nexpected:\n%s\nactual:\n%s", expectedDiff, actualDiff)
	}
}

func reviewGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	_ = reviewGitOutput(t, repo, args...)
}

func reviewGitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
