package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const projectWorkspaceDirectory = "projects"

type ProjectWorkspaceLockStore interface {
	AcquireWorkspaceBootstrapLock(context.Context, string) (store.WorkspaceBootstrapLock, error)
}

type ProjectMaterializer struct {
	locks         ProjectWorkspaceLockStore
	repositories  RepositoryResolver
	git           Git
	workspaceRoot string
}

func NewProjectMaterializer(lockStore ProjectWorkspaceLockStore, repositories RepositoryResolver, git Git, workspaceRoot string) (*ProjectMaterializer, error) {
	if lockStore == nil || repositories == nil || git == nil {
		return nil, fmt.Errorf("Project Workspace materializer dependencies: %w", ErrInvalidMetadata)
	}
	if strings.TrimSpace(workspaceRoot) == "" || !filepath.IsAbs(workspaceRoot) {
		return nil, ErrInvalidRoot
	}
	return &ProjectMaterializer{locks: lockStore, repositories: repositories, git: git, workspaceRoot: filepath.Clean(workspaceRoot)}, nil
}

// EnsureProjectWorkspace returns the single durable accepted checkout for a
// Project. The configured repository is only a bootstrap source: once the
// checkout exists, its Git metadata remains the accepted Project identity even
// if Project configuration changes later.
func (m *ProjectMaterializer) EnsureProjectWorkspace(ctx context.Context, project store.Project) (result store.ProjectWorkspace, err error) {
	if strings.TrimSpace(project.ID) == "" || strings.TrimSpace(project.RepositoryPath) == "" || strings.TrimSpace(project.DefaultBranch) == "" {
		return store.ProjectWorkspace{}, ErrInvalidMetadata
	}

	lock, err := m.locks.AcquireWorkspaceBootstrapLock(ctx, "project:"+project.ID)
	if err != nil {
		return store.ProjectWorkspace{}, fmt.Errorf("acquire Project Workspace lock: %w", err)
	}
	defer func() {
		if releaseErr := lock.Release(); err == nil && releaseErr != nil {
			result = store.ProjectWorkspace{}
			err = fmt.Errorf("release Project Workspace lock: %w", releaseErr)
		}
	}()

	root, err := m.projectRoot()
	if err != nil {
		return store.ProjectWorkspace{}, err
	}
	finalPath, err := projectWorkspacePath(root, project.ID)
	if err != nil {
		return store.ProjectWorkspace{}, err
	}
	if err := cleanupProjectTemps(root, project.ID); err != nil {
		return store.ProjectWorkspace{}, fmt.Errorf("cleanup interrupted Project Workspace bootstrap: %w", err)
	}

	if existing, ok, err := m.inspectExisting(ctx, project.ID, finalPath); err != nil {
		return store.ProjectWorkspace{}, err
	} else if ok {
		return existing, nil
	}

	source, err := m.repositories.Resolve(project.RepositoryPath)
	if err != nil {
		return store.ProjectWorkspace{}, fmt.Errorf("%w: validate Project repository source: %v", ErrBootstrapFailed, err)
	}
	if err := m.git.ValidateBranch(ctx, project.DefaultBranch); err != nil {
		return store.ProjectWorkspace{}, fmt.Errorf("%w: validate Project Workspace branch: %v", ErrBootstrapFailed, err)
	}

	temporary, err := os.MkdirTemp(root, "."+project.ID+".bootstrap-")
	if err != nil {
		return store.ProjectWorkspace{}, fmt.Errorf("%w: create Project Workspace bootstrap directory: %v", ErrBootstrapFailed, err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(temporary)
		}
	}()

	if err := m.git.Clone(ctx, source, temporary, project.DefaultBranch); err != nil {
		return store.ProjectWorkspace{}, fmt.Errorf("%w: clone Project repository: %v", ErrBootstrapFailed, err)
	}
	if err := os.Rename(temporary, finalPath); err != nil {
		return store.ProjectWorkspace{}, fmt.Errorf("%w: publish Project Workspace checkout: %v", ErrBootstrapFailed, err)
	}
	published = true

	ready, ok, err := m.inspectExisting(ctx, project.ID, finalPath)
	if err != nil {
		return store.ProjectWorkspace{}, err
	}
	if !ok {
		return store.ProjectWorkspace{}, fmt.Errorf("%w: published Project Workspace disappeared", ErrBootstrapFailed)
	}
	return ready, nil
}

func (m *ProjectMaterializer) inspectExisting(ctx context.Context, projectID, path string) (store.ProjectWorkspace, bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return store.ProjectWorkspace{}, false, nil
	}
	if err != nil {
		return store.ProjectWorkspace{}, false, fmt.Errorf("%w: inspect Project Workspace: %v", ErrBootstrapFailed, err)
	}
	if !info.IsDir() {
		return store.ProjectWorkspace{}, false, fmt.Errorf("%w: Project Workspace path is not a directory", ErrBootstrapFailed)
	}
	isRepository, err := m.git.IsRepository(ctx, path)
	if err != nil {
		return store.ProjectWorkspace{}, false, fmt.Errorf("%w: inspect Project Workspace repository: %v", ErrBootstrapFailed, err)
	}
	if !isRepository {
		return store.ProjectWorkspace{}, false, fmt.Errorf("%w: Project Workspace path is not a Git repository", ErrBootstrapFailed)
	}
	branch, err := m.git.CurrentBranch(ctx, path)
	if err != nil || strings.TrimSpace(branch) == "" {
		return store.ProjectWorkspace{}, false, fmt.Errorf("%w: inspect Project Workspace branch: %v", ErrBootstrapFailed, err)
	}
	origin, err := m.git.OriginURL(ctx, path)
	if err != nil || strings.TrimSpace(origin) == "" {
		return store.ProjectWorkspace{}, false, fmt.Errorf("%w: inspect Project Workspace origin: %v", ErrBootstrapFailed, err)
	}
	revision, err := m.git.HeadRevision(ctx, path)
	if err != nil || strings.TrimSpace(revision) == "" {
		return store.ProjectWorkspace{}, false, fmt.Errorf("%w: inspect Project Workspace revision: %v", ErrBootstrapFailed, err)
	}
	return store.ProjectWorkspace{ProjectID: projectID, Path: path, RepositoryPath: origin, BaseBranch: branch, AcceptedRevision: revision}, true, nil
}

func (m *ProjectMaterializer) projectRoot() (string, error) {
	if err := os.MkdirAll(m.workspaceRoot, 0o750); err != nil {
		return "", fmt.Errorf("create Workspace root: %w", err)
	}
	root, err := filepath.EvalSymlinks(m.workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("canonicalize Workspace root: %w", err)
	}
	projects := filepath.Join(root, projectWorkspaceDirectory)
	if err := os.MkdirAll(projects, 0o750); err != nil {
		return "", fmt.Errorf("create Project Workspace root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(projects)
	if err != nil {
		return "", fmt.Errorf("canonicalize Project Workspace root: %w", err)
	}
	return resolved, nil
}

func projectWorkspacePath(root, projectID string) (string, error) {
	if projectID == "" || projectID != filepath.Base(projectID) || strings.ContainsAny(projectID, `/\`) {
		return "", fmt.Errorf("Project id %q: %w", projectID, ErrInvalidMetadata)
	}
	candidate := filepath.Join(root, projectID)
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("Project Workspace path: %w", ErrInvalidMetadata)
	}
	return candidate, nil
}

func cleanupProjectTemps(root, projectID string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	prefix := "." + projectID + ".bootstrap-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
