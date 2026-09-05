package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	workspacepkg "github.com/brantje/agent-board/apps/server/internal/workspace"
)

type workspaceLookupFake struct {
	project      store.Project
	issue        store.Issue
	workspace    store.Workspace
	projectErr   error
	issueErr     error
	workspaceErr error
}

func (f *workspaceLookupFake) GetProject(context.Context, string) (store.Project, error) {
	return f.project, f.projectErr
}
func (f *workspaceLookupFake) GetIssue(context.Context, string, string) (store.Issue, error) {
	return f.issue, f.issueErr
}
func (f *workspaceLookupFake) GetWorkspaceByIssue(context.Context, string, string) (store.Workspace, error) {
	return f.workspace, f.workspaceErr
}

type workspaceMaterializerFunc func(context.Context, store.Project, store.Issue, store.Workspace) (store.Workspace, error)

func (f workspaceMaterializerFunc) Ensure(ctx context.Context, project store.Project, issue store.Issue, workspace store.Workspace) (store.Workspace, error) {
	return f(ctx, project, issue, workspace)
}

func TestWorkspaceServiceEnsuresReservedWorkspace(t *testing.T) {
	lookup := &workspaceLookupFake{
		project:   store.Project{ID: "project-1"},
		issue:     store.Issue{ID: "issue-1", ProjectID: "project-1"},
		workspace: store.Workspace{ID: "workspace-1", ProjectID: "project-1", IssueID: "issue-1", BootstrapStatus: "PENDING"},
	}
	called := false
	materializer := workspaceMaterializerFunc(func(_ context.Context, project store.Project, issue store.Issue, workspace store.Workspace) (store.Workspace, error) {
		called = true
		if project.ID != lookup.project.ID || issue.ID != lookup.issue.ID || workspace.ID != lookup.workspace.ID {
			t.Fatalf("materializer received wrong identity: project=%+v issue=%+v workspace=%+v", project, issue, workspace)
		}
		workspace.BootstrapStatus = "READY"
		workspace.Path = "/workspaces/workspace-1"
		return workspace, nil
	})
	service, err := NewWorkspaceService(lookup, materializer)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.EnsureIssueWorkspace(context.Background(), "project-1", "issue-1")
	if err != nil {
		t.Fatalf("EnsureIssueWorkspace() error = %v", err)
	}
	if !called || got.ID != "workspace-1" || got.BootstrapStatus != "READY" {
		t.Fatalf("EnsureIssueWorkspace() = %+v called=%v", got, called)
	}
}

func TestWorkspaceServiceMapsScopedLookupAndBootstrapErrors(t *testing.T) {
	tests := []struct {
		name       string
		lookup     *workspaceLookupFake
		materialErr error
		wantCode   string
	}{
		{
			name:     "project missing",
			lookup:   &workspaceLookupFake{projectErr: store.ErrNotFound},
			wantCode: "project_not_found",
		},
		{
			name: "issue missing",
			lookup: &workspaceLookupFake{
				project:  store.Project{ID: "project-1"},
				issueErr: store.ErrNotFound,
			},
			wantCode: "issue_not_found",
		},
		{
			name: "workspace missing",
			lookup: &workspaceLookupFake{
				project:      store.Project{ID: "project-1"},
				issue:        store.Issue{ID: "issue-1", ProjectID: "project-1"},
				workspaceErr: store.ErrNotFound,
			},
			wantCode: "workspace_not_found",
		},
		{
			name: "bootstrap failed",
			lookup: &workspaceLookupFake{
				project:   store.Project{ID: "project-1"},
				issue:     store.Issue{ID: "issue-1", ProjectID: "project-1"},
				workspace: store.Workspace{ID: "workspace-1", ProjectID: "project-1", IssueID: "issue-1"},
			},
			materialErr: workspacepkg.ErrBootstrapFailed,
			wantCode:    "workspace_bootstrap_failed",
		},
		{
			name: "configuration invalid",
			lookup: &workspaceLookupFake{
				project:   store.Project{ID: "project-1"},
				issue:     store.Issue{ID: "issue-1", ProjectID: "project-1"},
				workspace: store.Workspace{ID: "workspace-1", ProjectID: "project-1", IssueID: "issue-1"},
			},
			materialErr: workspacepkg.ErrInvalidMetadata,
			wantCode:    "workspace_configuration_invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			materializer := workspaceMaterializerFunc(func(context.Context, store.Project, store.Issue, store.Workspace) (store.Workspace, error) {
				return store.Workspace{}, tc.materialErr
			})
			service, err := NewWorkspaceService(tc.lookup, materializer)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.EnsureIssueWorkspace(context.Background(), "project-1", "issue-1")
			if err == nil {
				t.Fatal("expected error")
			}
			apiErr, ok := AsError(err)
			if !ok || apiErr.Code != tc.wantCode {
				t.Fatalf("error = %#v, want app error %q", err, tc.wantCode)
			}
		})
	}
}

func TestNewWorkspaceServiceRequiresDependencies(t *testing.T) {
	materializer := workspaceMaterializerFunc(func(context.Context, store.Project, store.Issue, store.Workspace) (store.Workspace, error) {
		return store.Workspace{}, errors.New("unused")
	})
	if _, err := NewWorkspaceService(nil, materializer); err == nil {
		t.Fatal("nil store unexpectedly accepted")
	}
	if _, err := NewWorkspaceService(&workspaceLookupFake{}, nil); err == nil {
		t.Fatal("nil materializer unexpectedly accepted")
	}
}
