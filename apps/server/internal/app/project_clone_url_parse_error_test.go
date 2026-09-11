package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCreateGitProjectRejectsCredentialsWhenURLParserRejectsPath(t *testing.T) {
	service := New(&recordingProjectStore{})
	cloneURL := "https://user:token@example.com/%zz"

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

func TestCreateGitProjectAllowsParseHostileCredentialFreeCloneString(t *testing.T) {
	service := New(&recordingProjectStore{})
	cloneURL := "https://example.com/%zz"

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
