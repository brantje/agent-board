package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type projectInspectionGit struct {
	Git
	isRepository  bool
	repositoryErr error
	branch        string
	branchErr     error
	origin        string
	originErr     error
	revision      string
	revisionErr   error
}

func (g projectInspectionGit) IsRepository(context.Context, string) (bool, error) {
	return g.isRepository, g.repositoryErr
}

func (g projectInspectionGit) CurrentBranch(context.Context, string) (string, error) {
	return g.branch, g.branchErr
}

func (g projectInspectionGit) OriginURL(context.Context, string) (string, error) {
	return g.origin, g.originErr
}

func (g projectInspectionGit) HeadRevision(context.Context, string) (string, error) {
	return g.revision, g.revisionErr
}

func TestProjectMaterializerInspectExistingRejectsGitIdentityFailures(t *testing.T) {
	path := t.TempDir()
	cause := errors.New("git inspection failed")
	base := projectInspectionGit{
		isRepository: true,
		branch:       "main",
		origin:       "/repo",
		revision:     "abc123",
	}

	cases := []struct {
		name    string
		git     projectInspectionGit
		message string
	}{
		{name: "repository inspection", git: func() projectInspectionGit { value := base; value.repositoryErr = cause; return value }(), message: "repository"},
		{name: "branch inspection", git: func() projectInspectionGit { value := base; value.branchErr = cause; return value }(), message: "branch"},
		{name: "blank branch", git: func() projectInspectionGit { value := base; value.branch = " "; return value }(), message: "branch"},
		{name: "origin inspection", git: func() projectInspectionGit { value := base; value.originErr = cause; return value }(), message: "origin"},
		{name: "blank origin", git: func() projectInspectionGit { value := base; value.origin = " "; return value }(), message: "origin"},
		{name: "revision inspection", git: func() projectInspectionGit { value := base; value.revisionErr = cause; return value }(), message: "revision"},
		{name: "blank revision", git: func() projectInspectionGit { value := base; value.revision = " "; return value }(), message: "revision"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			materializer := &ProjectMaterializer{git: tc.git}
			_, found, err := materializer.inspectExisting(t.Context(), "project-1", path)
			if found || !errors.Is(err, ErrBootstrapFailed) || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("inspectExisting() found=%v error=%v", found, err)
			}
		})
	}
}

func TestProjectMaterializerProjectRootReportsFilesystemConflicts(t *testing.T) {
	fileRoot := filepath.Join(t.TempDir(), "workspace-root")
	if err := os.WriteFile(fileRoot, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	materializer := &ProjectMaterializer{workspaceRoot: fileRoot}
	if _, err := materializer.projectRoot(); err == nil || !strings.Contains(err.Error(), "create Workspace root") {
		t.Fatalf("file root error=%v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, projectWorkspaceDirectory), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	materializer = &ProjectMaterializer{workspaceRoot: root}
	if _, err := materializer.projectRoot(); err == nil || !strings.Contains(err.Error(), "create Project Workspace root") {
		t.Fatalf("projects path conflict error=%v", err)
	}
}
