package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCreateGitProjectRejectsCredentialBearingHTTPCloneURL(t *testing.T) {
	tests := []string{
		"https://token@example.com/acme/widget.git",
		"https://user:secret@example.com/acme/widget.git",
		"http://user@example.com/acme/widget.git",
	}
	for _, cloneURL := range tests {
		t.Run(cloneURL, func(t *testing.T) {
			service := New(&recordingProjectStore{})
			_, err := service.CreateProject(context.Background(), store.Project{
				Name:        "Widget",
				IssuePrefix: "WG",
				SourceType:  store.ProjectSourceGit,
				CloneURL:    stringPointer(cloneURL),
			})
			assertInvalidProjectSource(t, err)
		})
	}
}

func TestUpdateGitProjectRejectsCredentialBearingHTTPCloneURL(t *testing.T) {
	service := New(&recordingProjectStore{})
	_, err := service.UpdateProject(context.Background(), store.Project{
		ID:          "project-1",
		Name:        "Widget",
		IssuePrefix: "WG",
		SourceType:  store.ProjectSourceGit,
		CloneURL:    stringPointer("https://token@example.com/acme/widget.git"),
	})
	assertInvalidProjectSource(t, err)
}

func TestCreateGitProjectAllowsSSHCloneURLSyntax(t *testing.T) {
	for _, cloneURL := range []string{
		"git@example.com:acme/widget.git",
		"ssh://git@example.com/acme/widget.git",
	} {
		t.Run(cloneURL, func(t *testing.T) {
			service := New(&recordingProjectStore{})
			project, err := service.CreateProject(context.Background(), store.Project{
				Name:        "Widget",
				IssuePrefix: "WG",
				SourceType:  store.ProjectSourceGit,
				CloneURL:    stringPointer(cloneURL),
			})
			if err != nil {
				t.Fatalf("CreateProject() error = %v", err)
			}
			if project.CloneURL == nil || *project.CloneURL != cloneURL {
				t.Fatalf("CreateProject() cloneURL = %v, want %q", project.CloneURL, cloneURL)
			}
		})
	}
}

func TestUpdateLocalProjectToGitClearsLocalFields(t *testing.T) {
	service := New(&recordingProjectStore{})
	project, err := service.UpdateProject(context.Background(), store.Project{
		ID:             "project-1",
		Name:           "Widget",
		IssuePrefix:    "WG",
		SourceType:     store.ProjectSourceGit,
		CloneURL:       stringPointer("https://example.com/acme/widget.git"),
		RepositoryPath: "/repositories/widget",
		DefaultBranch:  "main",
	})
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}
	if project.RepositoryPath != "" || project.DefaultBranch != "" {
		t.Fatalf("git Project retained local fields: repositoryPath=%q defaultBranch=%q", project.RepositoryPath, project.DefaultBranch)
	}
}

func assertInvalidProjectSource(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("project source validation error = nil")
	}
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "invalid_argument" {
		t.Fatalf("project source validation error = %v, want invalid_argument", err)
	}
}
