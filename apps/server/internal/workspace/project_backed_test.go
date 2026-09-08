package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type staticProjectWorkspaceSource struct {
	value store.ProjectWorkspace
	err   error
}

func (s staticProjectWorkspaceSource) EnsureProjectWorkspace(context.Context, store.Project) (store.ProjectWorkspace, error) {
	return s.value, s.err
}

func TestProjectBackedMaterializerUsesAcceptedProjectWorkspace(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git.GitCLI, sourceRoot)
	policy, _ := repository.NewPolicy([]string{sourceRoot})
	projectMaterializer, _ := NewProjectMaterializer(&projectWorkspaceLockStore{}, requireProvisioner(t, policy, git), git, filepath.Join(parent, "project-workspaces"))
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	accepted, err := projectMaterializer.EnsureProjectWorkspace(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(accepted.Path, "accepted.txt"), []byte("accepted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(context.Background(), "-C", accepted.Path, "add", "accepted.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(context.Background(), "-C", accepted.Path, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "accepted"); err != nil {
		t.Fatal(err)
	}
	accepted.AcceptedRevision, err = git.HeadRevision(context.Background(), accepted.Path)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate another approval advancing the mutable accepted branch after the
	// accepted snapshot was resolved but before this Issue Workspace clones it.
	if err := os.WriteFile(filepath.Join(accepted.Path, "newer.txt"), []byte("newer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(context.Background(), "-C", accepted.Path, "add", "newer.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(context.Background(), "-C", accepted.Path, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "newer accepted state"); err != nil {
		t.Fatal(err)
	}
	newerRevision, err := git.HeadRevision(context.Background(), accepted.Path)
	if err != nil {
		t.Fatal(err)
	}
	if newerRevision == accepted.AcceptedRevision {
		t.Fatal("fixture did not advance accepted branch")
	}

	issueRoot := filepath.Join(parent, "issue-workspaces")
	state := &memoryStateStore{workspace: fixtureWorkspace(source)}
	legacy, _ := NewMaterializer(state, policy, git, issueRoot)
	backed, err := NewProjectBackedMaterializer(legacy, staticProjectWorkspaceSource{value: accepted})
	if err != nil {
		t.Fatal(err)
	}
	issue := store.Issue{ID: "issue-1", ProjectID: project.ID}
	got, err := backed.Ensure(context.Background(), project, issue, state.workspace)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(got.Path, "accepted.txt"))
	if err != nil || string(content) != "accepted\n" {
		t.Fatalf("Issue Workspace did not start from accepted Project Workspace: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(got.Path, "newer.txt")); !os.IsNotExist(err) {
		t.Fatalf("Issue Workspace included state newer than accepted revision: err=%v", err)
	}
	if got.BaseRevision == nil || *got.BaseRevision != accepted.AcceptedRevision {
		t.Fatalf("Issue Workspace base revision=%v, want accepted revision %q", got.BaseRevision, accepted.AcceptedRevision)
	}
}

func TestProjectBackedMaterializerKeepsPersistedPendingRevisionAcrossRetry(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git.GitCLI, sourceRoot)
	policy, _ := repository.NewPolicy([]string{sourceRoot})
	projectMaterializer, _ := NewProjectMaterializer(&projectWorkspaceLockStore{}, requireProvisioner(t, policy, git), git, filepath.Join(parent, "project-workspaces"))
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	accepted, err := projectMaterializer.EnsureProjectWorkspace(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(accepted.Path, "pinned.txt"), []byte("pinned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(context.Background(), "-C", accepted.Path, "add", "pinned.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(context.Background(), "-C", accepted.Path, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "pinned accepted state"); err != nil {
		t.Fatal(err)
	}
	pinnedRevision, err := git.HeadRevision(context.Background(), accepted.Path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(accepted.Path, "after-restart.txt"), []byte("newer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(context.Background(), "-C", accepted.Path, "add", "after-restart.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(context.Background(), "-C", accepted.Path, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "accepted after bootstrap crash"); err != nil {
		t.Fatal(err)
	}
	accepted.AcceptedRevision, err = git.HeadRevision(context.Background(), accepted.Path)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.AcceptedRevision == pinnedRevision {
		t.Fatal("fixture did not advance accepted revision")
	}

	current := fixtureWorkspace(accepted.Path)
	current.RepositoryPath = workspaceStringPointer(accepted.Path)
	current.BaseBranch = workspaceStringPointer(accepted.BaseBranch)
	current.BaseRevision = workspaceStringPointer(pinnedRevision)
	current.BootstrapStatus = "PENDING"
	state := &memoryStateStore{workspace: current}
	legacy, _ := NewMaterializer(state, policy, git, filepath.Join(parent, "issue-workspaces"))
	backed, err := NewProjectBackedMaterializer(legacy, staticProjectWorkspaceSource{value: accepted})
	if err != nil {
		t.Fatal(err)
	}
	issue := store.Issue{ID: "issue-1", ProjectID: project.ID}

	got, err := backed.Ensure(context.Background(), project, issue, current)
	if err != nil {
		t.Fatalf("retry Ensure() error = %v", err)
	}
	if got.BaseRevision == nil || *got.BaseRevision != pinnedRevision {
		t.Fatalf("retry base revision=%v want pinned %q", got.BaseRevision, pinnedRevision)
	}
	if _, err := os.Stat(filepath.Join(got.Path, "pinned.txt")); err != nil {
		t.Fatalf("pinned file missing after retry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got.Path, "after-restart.txt")); !os.IsNotExist(err) {
		t.Fatalf("retry drifted to newer accepted revision: err=%v", err)
	}
}

func TestProjectBackedMaterializerPreservesExistingReadyIssueWorkspace(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git.GitCLI, sourceRoot)
	policy, _ := repository.NewPolicy([]string{sourceRoot})
	state := &memoryStateStore{workspace: fixtureWorkspace(source)}
	legacy, _ := NewMaterializer(state, policy, git, filepath.Join(parent, "workspaces"))
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	issue := store.Issue{ID: "issue-1", ProjectID: project.ID}
	ready, err := legacy.Ensure(context.Background(), project, issue, state.workspace)
	if err != nil {
		t.Fatal(err)
	}

	backed, _ := NewProjectBackedMaterializer(legacy, staticProjectWorkspaceSource{err: ErrBootstrapFailed})
	got, err := backed.Ensure(context.Background(), project, issue, ready)
	if err != nil {
		t.Fatalf("READY legacy Workspace should remain usable: %v", err)
	}
	if got.Path != ready.Path {
		t.Fatalf("READY Workspace path changed: got=%q want=%q", got.Path, ready.Path)
	}
}
