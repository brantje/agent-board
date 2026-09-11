package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectSourceTransitionsPersistAfterReload(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	reload := func(id string) store.Project {
		t.Helper()
		project, err := s.GetProject(ctx, id)
		if err != nil {
			t.Fatalf("GetProject(%s): %v", id, err)
		}
		return project
	}
	assertGit := func(project store.Project, cloneURL string, ref *string) {
		t.Helper()
		if project.SourceType != store.ProjectSourceGit {
			t.Fatalf("sourceType=%q, want %q", project.SourceType, store.ProjectSourceGit)
		}
		if project.CloneURL == nil || *project.CloneURL != cloneURL {
			t.Fatalf("cloneURL=%v, want %q", project.CloneURL, cloneURL)
		}
		if ref == nil {
			if project.SourceRef != nil {
				t.Fatalf("sourceRef=%v, want nil", project.SourceRef)
			}
		} else if project.SourceRef == nil || *project.SourceRef != *ref {
			t.Fatalf("sourceRef=%v, want %q", project.SourceRef, *ref)
		}
		if project.RepositoryPath != "" || project.DefaultBranch != "" {
			t.Fatalf("git source retained local fields: repositoryPath=%q defaultBranch=%q", project.RepositoryPath, project.DefaultBranch)
		}
	}

	local, err := s.CreateProject(ctx, store.Project{
		Name:           "Transition Local",
		IssuePrefix:    "TRN",
		RepositoryPath: "/repo/transition-local",
		DefaultBranch:  "main",
	})
	if err != nil {
		t.Fatalf("CreateProject(local): %v", err)
	}
	local = reload(local.ID)
	if local.SourceType != store.ProjectSourceLocal || local.RepositoryPath != "/repo/transition-local" || local.DefaultBranch != "main" {
		t.Fatalf("reloaded local source=%#v", local)
	}

	cloneURL := "https://example.com/acme/transition.git"
	ref := "release/v1"
	local.SourceType = store.ProjectSourceGit
	local.CloneURL = &cloneURL
	local.SourceRef = &ref
	local.RepositoryPath = ""
	local.DefaultBranch = ""
	if _, err := s.UpdateProject(ctx, local); err != nil {
		t.Fatalf("UpdateProject(local -> git): %v", err)
	}
	gitProject := reload(local.ID)
	assertGit(gitProject, cloneURL, &ref)

	changedURL := "ssh://git@example.com/acme/transition.git"
	changedRef := "feature/source"
	gitProject.CloneURL = &changedURL
	gitProject.SourceRef = &changedRef
	if _, err := s.UpdateProject(ctx, gitProject); err != nil {
		t.Fatalf("UpdateProject(git -> changed git): %v", err)
	}
	gitProject = reload(gitProject.ID)
	assertGit(gitProject, changedURL, &changedRef)

	gitProject.SourceRef = nil
	if _, err := s.UpdateProject(ctx, gitProject); err != nil {
		t.Fatalf("UpdateProject(clear git ref): %v", err)
	}
	gitProject = reload(gitProject.ID)
	assertGit(gitProject, changedURL, nil)

	gitProject.SourceType = store.ProjectSourceLocal
	gitProject.CloneURL = nil
	gitProject.SourceRef = nil
	gitProject.RepositoryPath = "/repo/transition-local-again"
	gitProject.DefaultBranch = "main"
	if _, err := s.UpdateProject(ctx, gitProject); err != nil {
		t.Fatalf("UpdateProject(git -> local): %v", err)
	}
	localAgain := reload(gitProject.ID)
	if localAgain.SourceType != store.ProjectSourceLocal || localAgain.CloneURL != nil || localAgain.SourceRef != nil {
		t.Fatalf("reloaded git -> local source=%#v", localAgain)
	}
	if localAgain.RepositoryPath != "/repo/transition-local-again" || localAgain.DefaultBranch != "main" {
		t.Fatalf("reloaded local fields: repositoryPath=%q defaultBranch=%q", localAgain.RepositoryPath, localAgain.DefaultBranch)
	}

	noRefURL := "https://example.com/acme/default-ref.git"
	gitWithoutRef, err := s.CreateProject(ctx, store.Project{
		Name:        "Git Without Ref",
		IssuePrefix: "GNR",
		SourceType:  store.ProjectSourceGit,
		CloneURL:    &noRefURL,
	})
	if err != nil {
		t.Fatalf("CreateProject(git without ref): %v", err)
	}
	gitWithoutRef = reload(gitWithoutRef.ID)
	assertGit(gitWithoutRef, noRefURL, nil)
}
