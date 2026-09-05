package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type memoryStateStore struct {
	workspace store.Workspace
	failed    bool
}

func (s *memoryStateStore) MarkWorkspaceBootstrapPending(_ context.Context, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, workingBranch string) (store.Workspace, error) {
	s.workspace.ProjectID = projectID
	s.workspace.IssueID = issueID
	s.workspace.ID = workspaceID
	s.workspace.Path = path
	s.workspace.RepositoryPath = ptr(repositoryPath)
	s.workspace.BaseBranch = ptr(baseBranch)
	s.workspace.WorkingBranch = workingBranch
	s.workspace.BootstrapStatus = "PENDING"
	return s.workspace, nil
}
func (s *memoryStateStore) MarkWorkspaceBootstrapReady(_ context.Context, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, baseRevision, workingBranch string) (store.Workspace, error) {
	s.workspace.ProjectID = projectID
	s.workspace.IssueID = issueID
	s.workspace.ID = workspaceID
	s.workspace.Path = path
	s.workspace.RepositoryPath = ptr(repositoryPath)
	s.workspace.BaseBranch = ptr(baseBranch)
	s.workspace.BaseRevision = ptr(baseRevision)
	s.workspace.WorkingBranch = workingBranch
	s.workspace.BootstrapStatus = "READY"
	return s.workspace, nil
}
func (s *memoryStateStore) MarkWorkspaceBootstrapFailed(context.Context, string, string, string) (store.Workspace, error) {
	s.failed = true
	s.workspace.BootstrapStatus = "FAILED"
	return s.workspace, nil
}

func TestMaterializerClonesRealRepositoryAndCreatesIssueBranch(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	workspaceRoot := filepath.Join(parent, "workspaces")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git, sourceRoot)
	policy, err := repository.NewPolicy([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	state := &memoryStateStore{workspace: fixtureWorkspace()}
	materializer, err := NewMaterializer(state, policy, git, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}

	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	issue := store.Issue{ID: "issue-1", ProjectID: project.ID}
	got, err := materializer.Ensure(context.Background(), project, issue, state.workspace)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if got.BootstrapStatus != "READY" || got.BaseRevision == nil || *got.BaseRevision == "" {
		t.Fatalf("workspace = %+v, want READY with base revision", got)
	}
	content, err := os.ReadFile(filepath.Join(got.Path, "README.md"))
	if err != nil || string(content) != "fixture\n" {
		t.Fatalf("checkout README = %q, err=%v", content, err)
	}
	branch, err := git.CurrentBranch(context.Background(), got.Path)
	if err != nil || branch != got.WorkingBranch {
		t.Fatalf("branch = %q, err=%v, want %q", branch, err, got.WorkingBranch)
	}
}

func TestMaterializerRejectsNonGitRepositoryWithoutEmptyFallback(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	workspaceRoot := filepath.Join(parent, "workspaces")
	source := filepath.Join(sourceRoot, "not-git")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	policy, _ := repository.NewPolicy([]string{sourceRoot})
	state := &memoryStateStore{workspace: fixtureWorkspace()}
	materializer, _ := NewMaterializer(state, policy, git, workspaceRoot)

	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	issue := store.Issue{ID: "issue-1", ProjectID: project.ID}
	_, err := materializer.Ensure(context.Background(), project, issue, state.workspace)
	if !errors.Is(err, ErrBootstrapFailed) {
		t.Fatalf("Ensure() error = %v, want ErrBootstrapFailed", err)
	}
	if !state.failed {
		t.Fatal("failed bootstrap was not persisted")
	}
	entries, readErr := os.ReadDir(workspaceRoot)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if entry.Name() == state.workspace.ID {
			t.Fatal("failed bootstrap created an authoritative empty workspace")
		}
	}
}

func TestMaterializerRejectsMissingBaseBranch(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git, sourceRoot)
	policy, _ := repository.NewPolicy([]string{sourceRoot})
	state := &memoryStateStore{workspace: fixtureWorkspace()}
	materializer, _ := NewMaterializer(state, policy, git, filepath.Join(parent, "workspaces"))
	_, err := materializer.Ensure(context.Background(), store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "missing"}, store.Issue{ID: "issue-1", ProjectID: "project-1"}, state.workspace)
	if !errors.Is(err, ErrBootstrapFailed) || !strings.Contains(err.Error(), "clone repository") {
		t.Fatalf("Ensure() error = %v, want clone failure", err)
	}
}

func requireGit(t *testing.T) *GitCLI {
	t.Helper()
	git, err := NewGitCLI("git")
	if err != nil {
		t.Fatalf("git is required for workspace tests: %v", err)
	}
	return git
}

func createFixtureRepository(t *testing.T, git *GitCLI, root string) string {
	t.Helper()
	repo := filepath.Join(root, "fixture")
	runGit(t, git.binary, "init", "-b", "main", repo)
	runGit(t, git.binary, "-C", repo, "config", "user.name", "Agent Board Tests")
	runGit(t, git.binary, "-C", repo, "config", "user.email", "tests@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, git.binary, "-C", repo, "add", "README.md")
	runGit(t, git.binary, "-C", repo, "commit", "-m", "fixture")
	return repo
}

func runGit(t *testing.T, binary string, args ...string) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func fixtureWorkspace() store.Workspace {
	return store.Workspace{ID: "workspace-1", ProjectID: "project-1", IssueID: "issue-1", Path: "pending://workspace/issue-1", WorkingBranch: "agent-board/issue-issue-1", BootstrapStatus: "PENDING"}
}

func ptr(value string) *string { return &value }
