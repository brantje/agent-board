package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

type stubProjectRepositoryProvisioner struct {
	path string
	err  error
}

func (s stubProjectRepositoryProvisioner) EnsureProjectRepository(_ context.Context, _, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.path, nil
}

type recordingProjectStore struct {
	fakeStore
	created store.Project
}

func (s *recordingProjectStore) CreateProject(_ context.Context, input store.Project) (store.Project, error) {
	s.created = input
	input.ID = "project-1"
	return input, nil
}

func TestCreateProjectProvisionsMissingRepository(t *testing.T) {
	root := t.TempDir()
	policy, err := repository.NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := repository.NewProvisioner(policy, git)
	if err != nil {
		t.Fatal(err)
	}
	projectStore := &recordingProjectStore{}
	service := New(projectStore)
	service.SetProjectRepositoryProvisioner(provisioner)

	candidate := filepath.Join(root, "widget")
	project, err := service.CreateProject(context.Background(), store.Project{
		Name:           "Widget",
		IssuePrefix:    "WG",
		RepositoryPath: candidate,
		DefaultBranch:  "main",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	want, _ := filepath.EvalSymlinks(candidate)
	if project.RepositoryPath != want {
		t.Fatalf("CreateProject() repositoryPath = %q, want %q", project.RepositoryPath, want)
	}
	isRepo, err := git.IsRepository(context.Background(), want)
	if err != nil || !isRepo {
		t.Fatalf("provisioned repository missing: isRepo=%v err=%v", isRepo, err)
	}
}

func TestCreateProjectWithoutProvisionerSkipsFilesystem(t *testing.T) {
	projectStore := &recordingProjectStore{}
	service := New(projectStore)
	project, err := service.CreateProject(context.Background(), store.Project{
		Name:           "Widget",
		IssuePrefix:    "WG",
		RepositoryPath: "/repositories/widget",
		DefaultBranch:  "main",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if project.RepositoryPath != "/repositories/widget" {
		t.Fatalf("CreateProject() repositoryPath = %q", project.RepositoryPath)
	}
	if _, err := os.Stat("/repositories/widget"); !os.IsNotExist(err) {
		t.Fatal("CreateProject() without provisioner should not create filesystem paths")
	}
}

func TestCreateProjectTranslatesRepositoryProvisionerErrors(t *testing.T) {
	projectStore := &recordingProjectStore{}
	service := New(projectStore)
	service.SetProjectRepositoryProvisioner(stubProjectRepositoryProvisioner{})

	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "unauthorized", err: repository.ErrPathNotAuthorized, code: "repository_path_invalid"},
		{name: "unavailable", err: repository.ErrPathUnavailable, code: "repository_path_unavailable"},
		{name: "generic", err: errors.New("git init failed"), code: "repository_provision_failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service.SetProjectRepositoryProvisioner(stubProjectRepositoryProvisioner{err: tc.err})
			_, err := service.CreateProject(context.Background(), store.Project{
				Name:           "Widget",
				IssuePrefix:    "WG",
				RepositoryPath: "/repositories/widget",
				DefaultBranch:  "main",
			})
			if err == nil {
				t.Fatal("CreateProject() error = nil")
			}
			apiErr, ok := AsError(err)
			if !ok || apiErr.Code != tc.code {
				t.Fatalf("CreateProject() error = %v, want code %q", err, tc.code)
			}
		})
	}
}

func TestUpdateProjectUsesDefaultBranchWhenProvisioning(t *testing.T) {
	projectStore := &recordingProjectStore{}
	service := New(projectStore)
	service.SetProjectRepositoryProvisioner(stubProjectRepositoryProvisioner{path: "/repositories/widget"})

	project, err := service.UpdateProject(context.Background(), store.Project{
		ID:             "project-1",
		Name:           "Widget",
		IssuePrefix:    "WG",
		RepositoryPath: "/repositories/widget",
	})
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}
	if project.DefaultBranch != "main" {
		t.Fatalf("UpdateProject() defaultBranch = %q, want main", project.DefaultBranch)
	}
}
