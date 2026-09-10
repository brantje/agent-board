package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectSourcesRoundTripThroughStore(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	local, err := s.CreateProject(ctx, store.Project{
		Name:           "Local Source",
		IssuePrefix:    "LOC",
		RepositoryPath: "/repo/local",
		DefaultBranch:  "main",
	})
	if err != nil {
		t.Fatalf("CreateProject(local): %v", err)
	}
	if local.SourceType != store.ProjectSourceLocal || local.CloneURL != nil || local.SourceRef != nil {
		t.Fatalf("local source = %#v", local)
	}

	cloneURL := "https://example.com/acme/widget.git"
	ref := "release/v1"
	gitProject, err := s.CreateProject(ctx, store.Project{
		Name:        "Git Source",
		IssuePrefix: "GIT",
		SourceType:  store.ProjectSourceGit,
		CloneURL:    &cloneURL,
		SourceRef:   &ref,
	})
	if err != nil {
		t.Fatalf("CreateProject(git): %v", err)
	}
	assertGitProjectSource(t, gitProject, cloneURL, ref)

	reloaded, err := s.GetProject(ctx, gitProject.ID)
	if err != nil {
		t.Fatalf("GetProject(git): %v", err)
	}
	assertGitProjectSource(t, reloaded, cloneURL, ref)

	projects, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects(): %v", err)
	}
	var listed *store.Project
	for i := range projects {
		if projects[i].ID == gitProject.ID {
			listed = &projects[i]
			break
		}
	}
	if listed == nil {
		t.Fatal("git Project missing from ListProjects")
	}
	assertGitProjectSource(t, *listed, cloneURL, ref)

	updatedURL := "ssh://git@example.com/acme/widget.git"
	updatedRef := "feature/source"
	gitProject.CloneURL = &updatedURL
	gitProject.SourceRef = &updatedRef
	gitProject, err = s.UpdateProject(ctx, gitProject)
	if err != nil {
		t.Fatalf("UpdateProject(git): %v", err)
	}
	assertGitProjectSource(t, gitProject, updatedURL, updatedRef)

	gitProject.SourceType = store.ProjectSourceLocal
	gitProject.CloneURL = nil
	gitProject.SourceRef = nil
	gitProject.RepositoryPath = "/repo/imported"
	gitProject.DefaultBranch = "main"
	localAgain, err := s.UpdateProject(ctx, gitProject)
	if err != nil {
		t.Fatalf("UpdateProject(git -> local): %v", err)
	}
	if localAgain.SourceType != store.ProjectSourceLocal || localAgain.CloneURL != nil || localAgain.SourceRef != nil {
		t.Fatalf("updated local source = %#v", localAgain)
	}
	if localAgain.RepositoryPath != "/repo/imported" || localAgain.DefaultBranch != "main" {
		t.Fatalf("updated local fields = repositoryPath %q defaultBranch %q", localAgain.RepositoryPath, localAgain.DefaultBranch)
	}
}

func TestSchemaRejectsInvalidProjectSourceCombinations(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		query string
	}{
		{
			name: "git without clone URL",
			query: `INSERT INTO projects (name, issue_prefix, source_type, repository_path, default_branch)
				VALUES ('Missing clone', 'MC', 'git', '', '')`,
		},
		{
			name: "local with git fields",
			query: `INSERT INTO projects (name, issue_prefix, source_type, clone_url, repository_path, default_branch)
				VALUES ('Mixed source', 'MS', 'local', 'https://example.com/acme/widget.git', '/repo/mixed', 'main')`,
		},
		{
			name: "git with local fields",
			query: `INSERT INTO projects (name, issue_prefix, source_type, clone_url, repository_path, default_branch)
				VALUES ('Mixed git', 'MG', 'git', 'https://example.com/acme/widget.git', '/repo/mixed', 'main')`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, tc.query); err == nil {
				t.Fatal("expected invalid Project source combination to be rejected")
			}
		})
	}
}

func assertGitProjectSource(t *testing.T, project store.Project, cloneURL, ref string) {
	t.Helper()
	if project.SourceType != store.ProjectSourceGit {
		t.Fatalf("sourceType = %q, want %q", project.SourceType, store.ProjectSourceGit)
	}
	if project.CloneURL == nil || *project.CloneURL != cloneURL {
		t.Fatalf("cloneURL = %v, want %q", project.CloneURL, cloneURL)
	}
	if project.SourceRef == nil || *project.SourceRef != ref {
		t.Fatalf("sourceRef = %v, want %q", project.SourceRef, ref)
	}
	if project.RepositoryPath != "" || project.DefaultBranch != "" {
		t.Fatalf("git Project retained local source fields: repositoryPath=%q defaultBranch=%q", project.RepositoryPath, project.DefaultBranch)
	}
}
