package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reviewedRevisionFixture struct {
	git            *GitCLI
	project        store.Project
	accepted       store.ProjectWorkspace
	issuePath      string
	backed         *ProjectBackedMaterializer
	baseRevision   string
	reviewRevision string
	review         store.Review
}

func newReviewedRevisionFixture(t *testing.T) reviewedRevisionFixture {
	t.Helper()
	counting := requireGit(t)
	git := counting.GitCLI
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git, sourceRoot)
	policy, err := repository.NewPolicy([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	projectMaterializer, err := NewProjectMaterializer(&projectWorkspaceLockStore{}, requireProvisioner(t, policy, counting), counting, filepath.Join(parent, "project-workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	project := store.Project{ID: "project-1", SourceType: store.ProjectSourceLocal, RepositoryPath: source, DefaultBranch: "main"}
	accepted, err := projectMaterializer.EnsureProjectWorkspace(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}

	issuePath := filepath.Join(parent, "issue-workspace")
	if err := git.Clone(t.Context(), source, issuePath, "main"); err != nil {
		t.Fatal(err)
	}
	baseRevision, err := git.HeadRevision(t.Context(), issuePath)
	if err != nil {
		t.Fatal(err)
	}
	workingBranch := "agent-board/AB-2"
	if err := git.CheckoutNewBranch(t.Context(), issuePath, workingBranch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issuePath, "reviewed.txt"), []byte("reviewed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(t.Context(), "-C", issuePath, "add", "--", "reviewed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(t.Context(), "-C", issuePath, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "Reviewed change"); err != nil {
		t.Fatal(err)
	}
	reviewRevision, err := git.HeadRevision(t.Context(), issuePath)
	if err != nil {
		t.Fatal(err)
	}

	state := &reviewProjectBackedStore{memoryStateStore: &memoryStateStore{workspace: store.Workspace{
		ProjectID:       project.ID,
		IssueID:         "issue-2",
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
	review := store.Review{
		ID:             "review-2",
		ProjectID:      project.ID,
		IssueID:        "issue-2",
		BaseRevision:   baseRevision,
		ReviewRevision: reviewRevision,
	}
	return reviewedRevisionFixture{
		git:            git,
		project:        project,
		accepted:       accepted,
		issuePath:      issuePath,
		backed:         backed,
		baseRevision:   baseRevision,
		reviewRevision: reviewRevision,
		review:         review,
	}
}

func TestApplyReviewedRevisionIsIdempotentWhenTargetAlreadyAtReviewCommit(t *testing.T) {
	fixture := newReviewedRevisionFixture(t)
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "fetch", "--no-tags", "--", fixture.issuePath, "+"+fixture.reviewRevision+":refs/agent-board/test/already-delivered"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "merge", "--ff-only", fixture.reviewRevision); err != nil {
		t.Fatal(err)
	}

	revision, err := fixture.backed.ApplyReviewedRevision(t.Context(), fixture.project, fixture.review)
	if err != nil {
		t.Fatal(err)
	}
	if revision != fixture.reviewRevision {
		t.Fatalf("revision=%q want=%q", revision, fixture.reviewRevision)
	}
}

func TestApplyReviewedRevisionRejectsReviewThatDoesNotContainPinnedBase(t *testing.T) {
	fixture := newReviewedRevisionFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.accepted.Path, "target-only.txt"), []byte("target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "add", "--", "target-only.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "Target-only change"); err != nil {
		t.Fatal(err)
	}
	targetHead, err := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.review.BaseRevision = targetHead

	_, err = fixture.backed.ApplyReviewedRevision(t.Context(), fixture.project, fixture.review)
	if err == nil || !strings.Contains(err.Error(), "reviewed commit does not contain Review base") {
		t.Fatalf("ApplyReviewedRevision() error=%v", err)
	}
	after, headErr := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if headErr != nil {
		t.Fatal(headErr)
	}
	if after != targetHead {
		t.Fatalf("target advanced after rejected Review: got=%q want=%q", after, targetHead)
	}
}

func TestApplyReviewedRevisionRejectsTargetRewrittenPastReviewBase(t *testing.T) {
	fixture := newReviewedRevisionFixture(t)
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "checkout", "--orphan", "rewritten-main"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "rm", "-rf", "."); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.accepted.Path, "rewritten.txt"), []byte("rewritten\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "add", "--", "rewritten.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "Rewrite target"); err != nil {
		t.Fatal(err)
	}
	rewrittenHead, err := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = fixture.backed.ApplyReviewedRevision(t.Context(), fixture.project, fixture.review)
	if err == nil || !strings.Contains(err.Error(), "Project target no longer contains Review base") {
		t.Fatalf("ApplyReviewedRevision() error=%v", err)
	}
	after, headErr := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if headErr != nil {
		t.Fatal(headErr)
	}
	if after != rewrittenHead {
		t.Fatalf("rewritten target advanced after rejected Review: got=%q want=%q", after, rewrittenHead)
	}
}

func TestApplyReviewedRevisionRequiresTrustedGitIntegration(t *testing.T) {
	counting := requireGit(t)
	root := t.TempDir()
	policy, err := repository.NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	state := &reviewProjectBackedStore{memoryStateStore: &memoryStateStore{workspace: store.Workspace{
		ProjectID: "project-1",
		IssueID:   "issue-1",
		Path:      filepath.Join(root, "issue"),
	}}}
	materializer, err := NewMaterializer(state, policy, counting, filepath.Join(root, "issues"))
	if err != nil {
		t.Fatal(err)
	}
	backed, err := NewProjectBackedMaterializer(materializer, staticProjectWorkspaceSource{value: store.ProjectWorkspace{Path: filepath.Join(root, "accepted")}})
	if err != nil {
		t.Fatal(err)
	}
	review := store.Review{
		ID:             "review-1",
		ProjectID:      "project-1",
		IssueID:        "issue-1",
		BaseRevision:   strings.Repeat("a", 40),
		ReviewRevision: strings.Repeat("b", 40),
	}
	_, err = backed.ApplyReviewedRevision(t.Context(), store.Project{ID: "project-1", SourceType: store.ProjectSourceLocal}, review)
	if err == nil || !strings.Contains(err.Error(), "trusted Git integration unavailable") {
		t.Fatalf("ApplyReviewedRevision() error=%v", err)
	}
}
