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
	path  string
	err   error
	calls int
}

func (s *stubProjectRepositoryProvisioner) EnsureProjectRepository(_ context.Context, _, _ string) (string, error) {
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	return s.path, nil
}

type recordingProjectStore struct {
	fakeStore
	created store.Project
	updated store.Project
}

func (s *recordingProjectStore) CreateProject(_ context.Context, input store.Project) (store.Project, error) {
	s.created = input
	input.ID = "project-1"
	return input, nil
}

func (s *recordingProjectStore) UpdateProject(_ context.Context, input store.Project) (store.Project, error) {
	s.updated = input
	return input, nil
}

func stringPointer(value string) *string {
	return &value
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
	if project.SourceType != store.ProjectSourceLocal {
		t.Fatalf("CreateProject() sourceType = %q, want %q", project.SourceType, store.ProjectSourceLocal)
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
	if project.SourceType != store.ProjectSourceLocal {
		t.Fatalf("CreateProject() sourceType = %q, want %q", project.SourceType, store.ProjectSourceLocal)
	}
	if _, err := os.Stat("/repositories/widget"); !os.IsNotExist(err) {
		t.Fatal("CreateProject() without provisioner should not create filesystem paths")
	}
}

func TestCreateGitProjectSkipsLocalRepositoryProvisioner(t *testing.T) {
	projectStore := &recordingProjectStore{}
	provisioner := &stubProjectRepositoryProvisioner{path: "/should/not/be/used"}
	service := New(projectStore)
	service.SetProjectRepositoryProvisioner(provisioner)
	cloneURL := "  https://example.com/acme/widget.git  "
	ref := "  feature/source  "

	project, err := service.CreateProject(context.Background(), store.Project{
		Name:        "Widget",
		IssuePrefix: "WG",
		SourceType:  store.ProjectSourceGit,
		CloneURL:    &cloneURL,
		SourceRef:   &ref,
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if provisioner.calls != 0 {
		t.Fatalf("local repository provisioner calls = %d, want 0", provisioner.calls)
	}
	if project.RepositoryPath != "" || project.DefaultBranch != "" {
		t.Fatalf("git Project retained local fields: repositoryPath=%q defaultBranch=%q", project.RepositoryPath, project.DefaultBranch)
	}
	if project.CloneURL == nil || *project.CloneURL != "https://example.com/acme/widget.git" {
		t.Fatalf("CreateProject() cloneURL = %v", project.CloneURL)
	}
	if project.SourceRef == nil || *project.SourceRef != "feature/source" {
		t.Fatalf("CreateProject() sourceRef = %v", project.SourceRef)
	}
}

func TestCreateGitProjectRequiresCloneURL(t *testing.T) {
	projectStore := &recordingProjectStore{}
	provisioner := &stubProjectRepositoryProvisioner{path: "/should/not/be/used"}
	service := New(projectStore)
	service.SetProjectRepositoryProvisioner(provisioner)

	_, err := service.CreateProject(context.Background(), store.Project{
		Name:        "Widget",
		IssuePrefix: "WG",
		SourceType:  store.ProjectSourceGit,
	})
	if err == nil {
		t.Fatal("CreateProject() error = nil")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "invalid_argument" {
		t.Fatalf("CreateProject() error = %v, want invalid_argument", err)
	}
	if provisioner.calls != 0 {
		t.Fatalf("local repository provisioner calls = %d, want 0", provisioner.calls)
	}
}

func TestCreateGitProjectRejectsCredentialsInHTTPCloneURL(t *testing.T) {
	service := New(&recordingProjectStore{})
	cloneURL := "https://user:token@example.com/acme/widget.git"

	_, err := service.CreateProject(context.Background(), store.Project{
		Name:        "Widget",
		IssuePrefix: "WG",
		SourceType:  store.ProjectSourceGit,
		CloneURL:    &cloneURL,
	})
	if err == nil {
		t.Fatal("CreateProject() error = nil")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "invalid_argument" {
		t.Fatalf("CreateProject() error = %v, want invalid_argument", err)
	}
}

func TestCreateGitProjectAllowsSCPStyleCloneURL(t *testing.T) {
	service := New(&recordingProjectStore{})
	cloneURL := "git@example.com:acme/widget.git"

	project, err := service.CreateProject(context.Background(), store.Project{
		Name:        "Widget",
		IssuePrefix: "WG",
		SourceType:  store.ProjectSourceGit,
		CloneURL:    &cloneURL,
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if project.CloneURL == nil || *project.CloneURL != cloneURL {
		t.Fatalf("CreateProject() cloneURL = %v, want %q", project.CloneURL, cloneURL)
	}
}

func TestCreateProjectRejectsUnknownSourceType(t *testing.T) {
	service := New(&recordingProjectStore{})
	_, err := service.CreateProject(context.Background(), store.Project{
		Name:           "Widget",
		IssuePrefix:    "WG",
		SourceType:     "github",
		RepositoryPath: "/repositories/widget",
	})
	if err == nil {
		t.Fatal("CreateProject() error = nil")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "invalid_argument" {
		t.Fatalf("CreateProject() error = %v, want invalid_argument", err)
	}
}

func TestCreateLocalProjectClearsInactiveGitFields(t *testing.T) {
	projectStore := &recordingProjectStore{}
	service := New(projectStore)
	cloneURL := "https://example.com/acme/widget.git"
	ref := "main"

	project, err := service.CreateProject(context.Background(), store.Project{
		Name:           "Widget",
		IssuePrefix:    "WG",
		SourceType:     store.ProjectSourceLocal,
		CloneURL:       &cloneURL,
		SourceRef:      &ref,
		RepositoryPath: "/repositories/widget",
		DefaultBranch:  "main",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if project.CloneURL != nil || project.SourceRef != nil {
		t.Fatalf("local Project retained git fields: cloneURL=%v sourceRef=%v", project.CloneURL, project.SourceRef)
	}
}

func TestUpdateGitProjectToLocalClearsGitFields(t *testing.T) {
	projectStore := &recordingProjectStore{}
	service := New(projectStore)
	cloneURL := "https://example.com/acme/widget.git"
	ref := "feature/source"

	project, err := service.UpdateProject(context.Background(), store.Project{
		ID:             "project-1",
		Name:           "Widget",
		IssuePrefix:    "WG",
		SourceType:     store.ProjectSourceLocal,
		CloneURL:       &cloneURL,
		SourceRef:      &ref,
		RepositoryPath: "/repositories/widget",
		DefaultBranch:  "main",
	})
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}
	if project.CloneURL != nil || project.SourceRef != nil {
		t.Fatalf("local Project retained git fields: cloneURL=%v sourceRef=%v", project.CloneURL, project.SourceRef)
	}
}

func TestCreateProjectTranslatesRepositoryProvisionerErrors(t *testing.T) {
	projectStore := &recordingProjectStore{}
	service := New(projectStore)

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
			service.SetProjectRepositoryProvisioner(&stubProjectRepositoryProvisioner{err: tc.err})
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
	service.SetProjectRepositoryProvisioner(&stubProjectRepositoryProvisioner{path: "/repositories/widget"})

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
	if project.SourceType != store.ProjectSourceLocal {
		t.Fatalf("UpdateProject() sourceType = %q, want %q", project.SourceType, store.ProjectSourceLocal)
	}
}

func TestCreateGitProjectClearsBlankOptionalRef(t *testing.T) {
	projectStore := &recordingProjectStore{}
	service := New(projectStore)
	cloneURL := "https://example.com/acme/widget.git"

	project, err := service.CreateProject(context.Background(), store.Project{
		Name:        "Widget",
		IssuePrefix: "WG",
		SourceType:  store.ProjectSourceGit,
		CloneURL:    &cloneURL,
		SourceRef:   stringPointer("   "),
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if project.SourceRef != nil {
		t.Fatalf("CreateProject() sourceRef = %v, want nil", project.SourceRef)
	}
}
