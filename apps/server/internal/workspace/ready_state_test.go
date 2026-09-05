package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestMaterializerReadyWorkspacePreservesEngineGitState(t *testing.T) {
	ctx := context.Background()
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git.GitCLI, sourceRoot)
	policy, err := repository.NewPolicy([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	state := &memoryStateStore{workspace: fixtureWorkspace(source)}
	materializer, err := NewMaterializer(state, policy, git, filepath.Join(parent, "workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	issue := store.Issue{ID: "issue-1", ProjectID: project.ID}

	ready, err := materializer.Ensure(ctx, project, issue, state.workspace)
	if err != nil {
		t.Fatalf("first Ensure() error = %v", err)
	}
	if _, err := git.GitCLI.run(ctx, "-C", ready.Path, "checkout", "main"); err != nil {
		t.Fatalf("checkout alternate branch: %v", err)
	}
	if _, err := materializer.Ensure(ctx, project, issue, ready); err != nil {
		t.Fatalf("Ensure() after Engine branch change error = %v", err)
	}
	if _, err := git.GitCLI.run(ctx, "-C", ready.Path, "checkout", "--detach", "HEAD"); err != nil {
		t.Fatalf("detach HEAD: %v", err)
	}
	if _, err := materializer.Ensure(ctx, project, issue, ready); err != nil {
		t.Fatalf("Ensure() with detached HEAD error = %v", err)
	}
	if clones := git.clones.Load(); clones != 1 {
		t.Fatalf("READY Git state changes triggered %d clones, want 1", clones)
	}
}
