package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCreateGitProjectAllowsCredentialFreeSSHURL(t *testing.T) {
	service := New(&recordingProjectStore{})
	cloneURL := "ssh://git@example.com/acme/widget.git"

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
