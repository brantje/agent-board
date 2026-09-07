package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCandidateApplierTreatsPostCommitLockReleaseFailureAsAccepted(t *testing.T) {
	git := requireGit(t).GitCLI
	repo := createFixtureRepository(t, git, t.TempDir())
	releaseErr := errors.New("approval lock release failed")
	applier, err := NewCandidateApplier(
		candidateReleaseErrorLockStore{err: releaseErr},
		candidateProjectSource{workspace: store.ProjectWorkspace{ProjectID: "project-1", Path: repo}},
		git,
	)
	if err != nil {
		t.Fatal(err)
	}

	revision, err := applier.Apply(t.Context(), store.Project{ID: "project-1"}, "review-post-commit-release", AcceptedCandidate{
		Files: []CandidateFileSource{{
			Path: "accepted-post-commit.txt",
			Chunks: []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("accepted\n")), nil
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Apply() error=%v, want committed delivery to remain successful", err)
	}
	if strings.TrimSpace(revision) == "" {
		t.Fatal("Apply() returned an empty accepted revision")
	}

	foundRevision, found, err := git.findAcceptedReview(t.Context(), repo, "review-post-commit-release")
	if err != nil {
		t.Fatal(err)
	}
	if !found || foundRevision != revision {
		t.Fatalf("accepted review revision=%q found=%v want=%q", foundRevision, found, revision)
	}
	content, readErr := os.ReadFile(filepath.Join(repo, "accepted-post-commit.txt"))
	if readErr != nil || string(content) != "accepted\n" {
		t.Fatalf("accepted content=%q err=%v", content, readErr)
	}
	status, statusErr := git.run(t.Context(), "-C", repo, "status", "--porcelain")
	if statusErr != nil || status != "" {
		t.Fatalf("accepted checkout status=%q err=%v", status, statusErr)
	}
}
