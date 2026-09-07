package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type ProjectWorkspaceSource interface {
	EnsureProjectWorkspace(context.Context, store.Project) (store.ProjectWorkspace, error)
}

// ProjectBackedMaterializer keeps the mature Issue Workspace bootstrap logic
// while substituting the current accepted Project Workspace as the source for
// new Issue Workspaces.
type ProjectBackedMaterializer struct {
	issue    *Materializer
	projects ProjectWorkspaceSource
}

func NewProjectBackedMaterializer(issue *Materializer, projects ProjectWorkspaceSource) (*ProjectBackedMaterializer, error) {
	if issue == nil || projects == nil {
		return nil, fmt.Errorf("Project-backed materializer dependencies: %w", ErrInvalidMetadata)
	}
	return &ProjectBackedMaterializer{issue: issue, projects: projects}, nil
}

func (m *ProjectBackedMaterializer) Ensure(ctx context.Context, project store.Project, issue store.Issue, current store.Workspace) (store.Workspace, error) {
	// Workspaces already published before the Project Workspace layer existed
	// retain their recorded source snapshot. Never rewrite a READY checkout.
	if current.BootstrapStatus == "READY" {
		return m.issue.Ensure(ctx, project, issue, current)
	}

	accepted, err := m.projects.EnsureProjectWorkspace(ctx, project)
	if err != nil {
		return store.Workspace{}, err
	}
	if strings.TrimSpace(accepted.Path) == "" || strings.TrimSpace(accepted.BaseBranch) == "" || strings.TrimSpace(accepted.AcceptedRevision) == "" {
		return store.Workspace{}, fmt.Errorf("%w: Project Workspace is incomplete", ErrBootstrapFailed)
	}

	source := filepath.Clean(accepted.Path)
	branch := strings.TrimSpace(accepted.BaseBranch)
	revision := strings.TrimSpace(accepted.AcceptedRevision)
	backedStore := &projectBackedStateStore{StateStore: m.issue.store, source: source, branch: branch, revision: revision}
	delegated := &Materializer{store: backedStore, repositories: exactWorkspaceResolver{path: source}, git: m.issue.git, workspaceRoot: m.issue.workspaceRoot}
	current.RepositoryPath = workspaceStringPointer(source)
	current.BaseBranch = workspaceStringPointer(branch)
	current.BaseRevision = workspaceStringPointer(revision)

	project.RepositoryPath = source
	project.DefaultBranch = branch
	return delegated.Ensure(ctx, project, issue, current)
}

type projectBackedStateStore struct {
	StateStore
	source   string
	branch   string
	revision string
}

func (s *projectBackedStateStore) GetWorkspaceByIssue(ctx context.Context, projectID, issueID string) (store.Workspace, error) {
	value, err := s.StateStore.GetWorkspaceByIssue(ctx, projectID, issueID)
	if err != nil {
		return store.Workspace{}, err
	}
	if value.BootstrapStatus != "READY" {
		value.RepositoryPath = workspaceStringPointer(s.source)
		value.BaseBranch = workspaceStringPointer(s.branch)
		value.BaseRevision = workspaceStringPointer(s.revision)
	}
	return value, nil
}

type exactWorkspaceResolver struct{ path string }

func (r exactWorkspaceResolver) Resolve(candidate string) (string, error) {
	if filepath.Clean(candidate) != r.path {
		return "", fmt.Errorf("Project Workspace source mismatch")
	}
	return r.path, nil
}

func workspaceStringPointer(value string) *string { return &value }
