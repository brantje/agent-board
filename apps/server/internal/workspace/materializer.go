package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

var (
	ErrInvalidMetadata = errors.New("workspace: invalid metadata")
	ErrInvalidRoot     = errors.New("workspace: invalid workspace root")
	ErrBootstrapFailed = errors.New("workspace: bootstrap failed")
)

type RepositoryResolver interface {
	Resolve(string) (string, error)
}

type StateStore interface {
	MarkWorkspaceBootstrapPending(context.Context, string, string, string, string, string, string, string) (store.Workspace, error)
	MarkWorkspaceBootstrapReady(context.Context, string, string, string, string, string, string, string, string) (store.Workspace, error)
	MarkWorkspaceBootstrapFailed(context.Context, string, string, string) (store.Workspace, error)
}

type Materializer struct {
	store         StateStore
	repositories  RepositoryResolver
	git           Git
	workspaceRoot string
}

func NewMaterializer(stateStore StateStore, repositories RepositoryResolver, git Git, workspaceRoot string) (*Materializer, error) {
	if stateStore == nil || repositories == nil || git == nil {
		return nil, fmt.Errorf("materializer dependencies: %w", ErrInvalidMetadata)
	}
	if strings.TrimSpace(workspaceRoot) == "" || !filepath.IsAbs(workspaceRoot) {
		return nil, ErrInvalidRoot
	}
	return &Materializer{
		store:         stateStore,
		repositories:  repositories,
		git:           git,
		workspaceRoot: filepath.Clean(workspaceRoot),
	}, nil
}

func (m *Materializer) Ensure(ctx context.Context, project store.Project, issue store.Issue, current store.Workspace) (result store.Workspace, err error) {
	if err := validateIdentity(project, issue, current); err != nil {
		return store.Workspace{}, err
	}
	if current.BootstrapStatus == "READY" {
		return current, nil
	}

	source, err := m.repositories.Resolve(project.RepositoryPath)
	if err != nil {
		return m.fail(ctx, current, fmt.Errorf("validate repository source: %w", err))
	}
	if err := m.git.ValidateBranch(ctx, project.DefaultBranch); err != nil {
		return m.fail(ctx, current, fmt.Errorf("validate base branch: %w", err))
	}
	if err := m.git.ValidateBranch(ctx, current.WorkingBranch); err != nil {
		return m.fail(ctx, current, fmt.Errorf("validate working branch: %w", err))
	}

	root, err := m.canonicalWorkspaceRoot()
	if err != nil {
		return m.fail(ctx, current, err)
	}
	finalPath, err := workspacePath(root, current.ID)
	if err != nil {
		return m.fail(ctx, current, err)
	}

	pending, err := m.store.MarkWorkspaceBootstrapPending(ctx, project.ID, issue.ID, current.ID, finalPath, source, project.DefaultBranch, current.WorkingBranch)
	if err != nil {
		return store.Workspace{}, fmt.Errorf("mark workspace pending: %w", err)
	}
	current = pending

	temporary, err := os.MkdirTemp(root, "."+current.ID+".bootstrap-")
	if err != nil {
		return m.fail(ctx, current, fmt.Errorf("create bootstrap directory: %w", err))
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(temporary)
		}
	}()

	if err := m.git.Clone(ctx, source, temporary, project.DefaultBranch); err != nil {
		return m.fail(ctx, current, fmt.Errorf("clone repository: %w", err))
	}
	if err := m.git.CheckoutNewBranch(ctx, temporary, current.WorkingBranch); err != nil {
		return m.fail(ctx, current, fmt.Errorf("create working branch: %w", err))
	}
	baseRevision, err := m.git.HeadRevision(ctx, temporary)
	if err != nil {
		return m.fail(ctx, current, fmt.Errorf("resolve base revision: %w", err))
	}
	if err := os.Rename(temporary, finalPath); err != nil {
		return m.fail(ctx, current, fmt.Errorf("publish workspace checkout: %w", err))
	}
	published = true

	ready, err := m.store.MarkWorkspaceBootstrapReady(ctx, project.ID, issue.ID, current.ID, finalPath, source, project.DefaultBranch, baseRevision, current.WorkingBranch)
	if err != nil {
		return store.Workspace{}, fmt.Errorf("mark workspace ready: %w", err)
	}
	return ready, nil
}

func (m *Materializer) fail(ctx context.Context, current store.Workspace, cause error) (store.Workspace, error) {
	if current.ID != "" && current.ProjectID != "" && current.IssueID != "" {
		_, _ = m.store.MarkWorkspaceBootstrapFailed(ctx, current.ProjectID, current.IssueID, current.ID)
	}
	return store.Workspace{}, fmt.Errorf("%w: %w", ErrBootstrapFailed, cause)
}

func (m *Materializer) canonicalWorkspaceRoot() (string, error) {
	if err := os.MkdirAll(m.workspaceRoot, 0o750); err != nil {
		return "", fmt.Errorf("create workspace root: %w", err)
	}
	root, err := filepath.EvalSymlinks(m.workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", ErrInvalidRoot
	}
	return root, nil
}

func workspacePath(root, workspaceID string) (string, error) {
	if workspaceID == "" || workspaceID != filepath.Base(workspaceID) || strings.ContainsAny(workspaceID, `/\\`) {
		return "", fmt.Errorf("workspace id %q: %w", workspaceID, ErrInvalidMetadata)
	}
	candidate := filepath.Join(root, workspaceID)
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace path: %w", ErrInvalidMetadata)
	}
	return candidate, nil
}

func validateIdentity(project store.Project, issue store.Issue, current store.Workspace) error {
	if project.ID == "" || issue.ID == "" || issue.ProjectID != project.ID || current.ID == "" || current.ProjectID != project.ID || current.IssueID != issue.ID {
		return ErrInvalidMetadata
	}
	if strings.TrimSpace(project.RepositoryPath) == "" || strings.TrimSpace(project.DefaultBranch) == "" || strings.TrimSpace(current.WorkingBranch) == "" {
		return ErrInvalidMetadata
	}
	return nil
}
