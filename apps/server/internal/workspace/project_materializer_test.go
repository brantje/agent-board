package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

type projectWorkspaceErrorLockStore struct {
	acquireErr error
	releaseErr error
}

func (s projectWorkspaceErrorLockStore) AcquireWorkspaceBootstrapLock(context.Context, string) (store.WorkspaceBootstrapLock, error) {
	if s.acquireErr != nil {
		return nil, s.acquireErr
	}
	return projectWorkspaceErrorLock{err: s.releaseErr}, nil
}

type projectWorkspaceErrorLock struct{ err error }

func (l projectWorkspaceErrorLock) Release() error { return l.err }

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

func TestNewProjectMaterializerValidatesDependenciesAndRoot(t *testing.T) {
	git := requireGit(t)
	root := t.TempDir()
	policy, err := repository.NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	locks := &projectWorkspaceLockStore{}

	cases := []struct {
		name         string
		locks        ProjectWorkspaceLockStore
		repositories RepositoryResolver
		git          Git
		workspace    string
		want         error
	}{
		{name: "missing lock store", repositories: policy, git: git, workspace: root, want: ErrInvalidMetadata},
		{name: "missing repository resolver", locks: locks, git: git, workspace: root, want: ErrInvalidMetadata},
		{name: "missing git", locks: locks, repositories: policy, workspace: root, want: ErrInvalidMetadata},
		{name: "relative workspace root", locks: locks, repositories: policy, git: git, workspace: "relative", want: ErrInvalidRoot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewProjectMaterializer(tc.locks, tc.repositories, tc.git, tc.workspace)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewProjectMaterializer() error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestProjectMaterializerValidatesProjectAndLockLifecycle(t *testing.T) {
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
	workspaceRoot := filepath.Join(parent, "workspaces")
	materializer, err := NewProjectMaterializer(&projectWorkspaceLockStore{}, policy, git, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}

	for name, project := range map[string]store.Project{
		"missing id":         {RepositoryPath: source, DefaultBranch: "main"},
		"missing repository": {ID: "project-1", DefaultBranch: "main"},
		"missing branch":     {ID: "project-1", RepositoryPath: source},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := materializer.EnsureProjectWorkspace(t.Context(), project); !errors.Is(err, ErrInvalidMetadata) {
				t.Fatalf("EnsureProjectWorkspace() error=%v want ErrInvalidMetadata", err)
			}
		})
	}

	acquireErr := errors.New("lock unavailable")
	locked, err := NewProjectMaterializer(projectWorkspaceErrorLockStore{acquireErr: acquireErr}, policy, git, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	if _, err := locked.EnsureProjectWorkspace(t.Context(), project); !errors.Is(err, acquireErr) {
		t.Fatalf("lock acquisition error=%v", err)
	}

	releaseErr := errors.New("lock release failed")
	locked, err = NewProjectMaterializer(projectWorkspaceErrorLockStore{releaseErr: releaseErr}, policy, git, filepath.Join(parent, "release-workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := locked.EnsureProjectWorkspace(t.Context(), project)
	if !errors.Is(err, releaseErr) {
		t.Fatalf("lock release error=%v", err)
	}
	if result != (store.ProjectWorkspace{}) {
		t.Fatalf("result=%+v want zero value after release failure", result)
	}
}

func TestProjectMaterializerRejectsCorruptExistingWorkspace(t *testing.T) {
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
	root, err := materializer.projectRoot()
	if err != nil {
		t.Fatal(err)
	}
	path, err := projectWorkspacePath(root, project.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("not a checkout"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.EnsureProjectWorkspace(t.Context(), project); !errors.Is(err, ErrBootstrapFailed) || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file workspace error=%v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.EnsureProjectWorkspace(t.Context(), project); !errors.Is(err, ErrBootstrapFailed) || !strings.Contains(err.Error(), "not a Git repository") {
		t.Fatalf("non-repository workspace error=%v", err)
	}
}

func TestProjectWorkspacePathAndBootstrapCleanup(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"", ".", "..", "project/child", `project\\child`} {
		if _, err := projectWorkspacePath(root, id); !errors.Is(err, ErrInvalidMetadata) {
			t.Fatalf("projectWorkspacePath(%q) error=%v want ErrInvalidMetadata", id, err)
		}
	}
	path, err := projectWorkspacePath(root, "project-1")
	if err != nil || path != filepath.Join(root, "project-1") {
		t.Fatalf("projectWorkspacePath()=%q err=%v", path, err)
	}

	stale := filepath.Join(root, ".project-1.bootstrap-stale")
	keep := filepath.Join(root, ".project-2.bootstrap-keep")
	if err := os.Mkdir(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cleanupProjectTemps(root, "project-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale bootstrap still exists: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated bootstrap was removed: %v", err)
	}
}
