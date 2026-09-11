package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reviewProjectBackedStore struct {
	*memoryStateStore
	mu sync.Mutex
}

func (s *reviewProjectBackedStore) AcquireWorkspaceBootstrapLock(context.Context, string) (store.WorkspaceBootstrapLock, error) {
	s.mu.Lock()
	return memoryLock{mu: &s.mu}, nil
}

func TestProjectBackedMaterializerAppliesReviewedRevision(t *testing.T) {
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
	project := store.Project{ID: "project-1", SourceType: store.ProjectSourceLocal, RepositoryPath: source, DefaultBranch: "main"}
	accepted, err := projectMaterializer.EnsureProjectWorkspace(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}

	issuePath := filepath.Join(parent, "issue-workspace")
	if err := git.GitCLI.Clone(t.Context(), source, issuePath, "main"); err != nil {
		t.Fatal(err)
	}
	baseRevision, err := git.GitCLI.HeadRevision(t.Context(), issuePath)
	if err != nil {
		t.Fatal(err)
	}
	workingBranch := "agent-board/AB-1"
	if err := git.GitCLI.CheckoutNewBranch(t.Context(), issuePath, workingBranch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issuePath, "reviewed.txt"), []byte("reviewed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.GitCLI.run(t.Context(), "-C", issuePath, "add", "--", "reviewed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git.GitCLI.run(t.Context(), "-C", issuePath, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "Reviewed change"); err != nil {
		t.Fatal(err)
	}
	reviewRevision, err := git.GitCLI.HeadRevision(t.Context(), issuePath)
	if err != nil {
		t.Fatal(err)
	}

	state := &reviewProjectBackedStore{memoryStateStore: &memoryStateStore{workspace: store.Workspace{
		ProjectID:       project.ID,
		IssueID:         "issue-1",
		Path:            issuePath,
		WorkingBranch:   workingBranch,
		BootstrapStatus: "READY",
	}}}
	issueMaterializer, err := NewMaterializer(state, policy, git, filepath.Join(parent, "issues"))
	if err != nil {
		t.Fatal(err)
	}
	backed, err := NewProjectBackedMaterializer(issueMaterializer, staticProjectWorkspaceSource{value: accepted})
	if err != nil {
		t.Fatal(err)
	}
	review := store.Review{ID: "review-1", ProjectID: project.ID, IssueID: "issue-1", BaseRevision: baseRevision, ReviewRevision: reviewRevision}
	revision, err := backed.ApplyReviewedRevision(t.Context(), project, review)
	if err != nil {
		t.Fatalf("ApplyReviewedRevision() error=%v", err)
	}
	if revision == accepted.AcceptedRevision {
		t.Fatalf("accepted revision did not advance: %q", revision)
	}
	content, err := os.ReadFile(filepath.Join(accepted.Path, "reviewed.txt"))
	if err != nil || string(content) != "reviewed\n" {
		t.Fatalf("reviewed content=%q err=%v", content, err)
	}
	branch, err := git.GitCLI.CurrentBranch(t.Context(), accepted.Path)
	if err != nil || branch != "main" {
		t.Fatalf("accepted branch=%q err=%v", branch, err)
	}
	if _, err := git.GitCLI.run(t.Context(), "-C", accepted.Path, "merge-base", "--is-ancestor", reviewRevision, revision); err != nil {
		t.Fatalf("review revision is not contained by delivered revision: %v", err)
	}
}

func TestProjectBackedMaterializerReviewRevisionValidatesMetadata(t *testing.T) {
	var nilMaterializer *ProjectBackedMaterializer
	if _, err := nilMaterializer.ApplyReviewedRevision(t.Context(), store.Project{SourceType: store.ProjectSourceLocal}, store.Review{}); err == nil {
		t.Fatal("nil materializer should fail")
	}

	git := requireGit(t)
	root := t.TempDir()
	policy, err := repository.NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	state := &reviewProjectBackedStore{memoryStateStore: &memoryStateStore{}}
	issueMaterializer, err := NewMaterializer(state, policy, git, filepath.Join(root, "issues"))
	if err != nil {
		t.Fatal(err)
	}
	backed, err := NewProjectBackedMaterializer(issueMaterializer, staticProjectWorkspaceSource{})
	if err != nil {
		t.Fatal(err)
	}
	completeReview := store.Review{ID: "review", IssueID: "issue", BaseRevision: strings.Repeat("a", 40), ReviewRevision: strings.Repeat("b", 40)}
	if _, err := backed.ApplyReviewedRevision(t.Context(), store.Project{SourceType: store.ProjectSourceGit}, completeReview); err == nil {
		t.Fatal("remote Project delivery should fail")
	}
	if _, err := backed.ApplyReviewedRevision(t.Context(), store.Project{SourceType: store.ProjectSourceLocal}, store.Review{ID: "review"}); err == nil {
		t.Fatal("incomplete Review Git identity should fail")
	}
}
