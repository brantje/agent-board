package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCandidateApprovalRetryAfterRestartReusesAcceptanceCommit(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git.GitCLI, sourceRoot)
	policy, err := repository.NewPolicy([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	projectMaterializer, err := NewProjectMaterializer(&projectWorkspaceLockStore{}, requireProvisioner(t, policy, git), git, filepath.Join(parent, "project-workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	accepted, err := projectMaterializer.EnsureProjectWorkspace(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}

	candidate := AcceptedCandidate{Files: []CandidateFileSource{{
		Path: "accepted.txt",
		Chunks: []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("accepted candidate\n")), nil
		}},
	}}}
	first, err := NewCandidateApplier(&projectWorkspaceLockStore{}, staticProjectWorkspaceSource{value: accepted}, git.GitCLI)
	if err != nil {
		t.Fatal(err)
	}
	firstRevision, err := first.Apply(context.Background(), project, "review-restart", candidate)
	if err != nil {
		t.Fatalf("first approval: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(accepted.Path, "accepted.txt"))
	if err != nil || string(content) != "accepted candidate\n" {
		t.Fatalf("accepted content=%q err=%v", content, err)
	}

	// A new applier represents a restarted process. Its candidate source fails on
	// use so the retry proves idempotency is decided from the durable Git commit,
	// not from re-reading or reapplying the reviewed candidate.
	restartedCandidate := AcceptedCandidate{Files: []CandidateFileSource{{
		Path: "accepted.txt",
		Chunks: []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
			return nil, errors.New("candidate blob should not be reopened after restart")
		}},
	}}}
	restarted, err := NewCandidateApplier(&projectWorkspaceLockStore{}, staticProjectWorkspaceSource{value: accepted}, git.GitCLI)
	if err != nil {
		t.Fatal(err)
	}
	secondRevision, err := restarted.Apply(context.Background(), project, "review-restart", restartedCandidate)
	if err != nil {
		t.Fatalf("approval retry after restart: %v", err)
	}
	if secondRevision != firstRevision {
		t.Fatalf("retry revision=%q want=%q", secondRevision, firstRevision)
	}

	log, err := git.GitCLI.run(context.Background(), "-C", accepted.Path, "log", "--fixed-strings", "--grep", "Agent-Board-Review: review-restart", "--format=%H")
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Fields(log); len(lines) != 1 || lines[0] != firstRevision {
		t.Fatalf("acceptance commits=%q want one commit %q", log, firstRevision)
	}
}
