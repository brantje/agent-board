package workspace

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type projectWorkspaceLockStore struct{ mu sync.Mutex }

func (s *projectWorkspaceLockStore) AcquireWorkspaceBootstrapLock(context.Context, string) (store.WorkspaceBootstrapLock, error) {
	s.mu.Lock()
	return memoryLock{mu: &s.mu}, nil
}

func TestProjectMaterializerCreatesDurableAcceptedCheckout(t *testing.T) {
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
	materializer, err := NewProjectMaterializer(&projectWorkspaceLockStore{}, policy, git, filepath.Join(parent, "workspaces"))
	if err != nil {
		t.Fatal(err)
	}

	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	got, err := materializer.EnsureProjectWorkspace(context.Background(), project)
	if err != nil {
		t.Fatalf("EnsureProjectWorkspace() error = %v", err)
	}
	if got.Path == "" || got.AcceptedRevision == "" || got.ProjectID != project.ID {
		t.Fatalf("Project Workspace = %+v, want durable checkout", got)
	}
	if filepath.Base(filepath.Dir(got.Path)) != projectWorkspaceDirectory {
		t.Fatalf("Project Workspace path = %q, want projects subdirectory", got.Path)
	}
	content, err := os.ReadFile(filepath.Join(got.Path, "README.md"))
	if err != nil || string(content) != "fixture\n" {
		t.Fatalf("Project Workspace README = %q, err=%v", content, err)
	}
	head, err := git.HeadRevision(context.Background(), got.Path)
	if err != nil || head != got.AcceptedRevision {
		t.Fatalf("Project Workspace HEAD = %q err=%v accepted=%q", head, err, got.AcceptedRevision)
	}
}

func TestProjectMaterializerPreservesInitializedRepositorySnapshot(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git.GitCLI, sourceRoot)
	policy, _ := repository.NewPolicy([]string{sourceRoot})
	materializer, _ := NewProjectMaterializer(&projectWorkspaceLockStore{}, policy, git, filepath.Join(parent, "workspaces"))

	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	first, err := materializer.EnsureProjectWorkspace(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	changed := project
	changed.RepositoryPath = filepath.Join(sourceRoot, "missing")
	changed.DefaultBranch = "changed"
	second, err := materializer.EnsureProjectWorkspace(context.Background(), changed)
	if err != nil {
		t.Fatalf("existing Project Workspace should ignore later Project source edits: %v", err)
	}
	if first.Path != second.Path || first.AcceptedRevision != second.AcceptedRevision || first.RepositoryPath != second.RepositoryPath {
		t.Fatalf("Project Workspace changed after Project configuration edit: first=%+v second=%+v", first, second)
	}
}
