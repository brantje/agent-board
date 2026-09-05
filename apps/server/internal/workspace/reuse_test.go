package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestMaterializerReadyWorkspaceReusesWorkingTreeState(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git.GitCLI, sourceRoot)
	policy, _ := repository.NewPolicy([]string{sourceRoot})
	state := &memoryStateStore{workspace: fixtureWorkspace(source)}
	materializer, _ := NewMaterializer(state, policy, git, filepath.Join(parent, "workspaces"))
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	issue := store.Issue{ID: "issue-1", ProjectID: project.ID}

	ready, err := materializer.Ensure(context.Background(), project, issue, state.workspace)
	if err != nil {
		t.Fatalf("first Ensure() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(ready.Path, "README.md"), []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ready.Path, "untracked.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reused, err := materializer.Ensure(context.Background(), project, issue, ready)
	if err != nil {
		t.Fatalf("second Ensure() error = %v", err)
	}
	if reused.ID != ready.ID || reused.Path != ready.Path {
		t.Fatalf("workspace identity changed: first=%+v second=%+v", ready, reused)
	}
	modified, err := os.ReadFile(filepath.Join(reused.Path, "README.md"))
	if err != nil || string(modified) != "modified\n" {
		t.Fatalf("modified file lost: content=%q err=%v", modified, err)
	}
	untracked, err := os.ReadFile(filepath.Join(reused.Path, "untracked.txt"))
	if err != nil || string(untracked) != "keep me\n" {
		t.Fatalf("untracked file lost: content=%q err=%v", untracked, err)
	}
	if clones := git.clones.Load(); clones != 1 {
		t.Fatalf("clone count = %d, want 1 across attempts", clones)
	}
}

func TestMaterializerRejectsRepositoryOutsideAuthorizedRoots(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	allowed := filepath.Join(parent, "allowed")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git.GitCLI, outside)
	policy, _ := repository.NewPolicy([]string{allowed})
	state := &memoryStateStore{workspace: fixtureWorkspace(source)}
	materializer, _ := NewMaterializer(state, policy, git, filepath.Join(parent, "workspaces"))
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	issue := store.Issue{ID: "issue-1", ProjectID: project.ID}

	_, err := materializer.Ensure(context.Background(), project, issue, state.workspace)
	if !errors.Is(err, ErrBootstrapFailed) {
		t.Fatalf("Ensure() error = %v, want ErrBootstrapFailed", err)
	}
	if !state.failed {
		t.Fatal("unauthorized source was not persisted as failed bootstrap")
	}
	if clones := git.clones.Load(); clones != 0 {
		t.Fatalf("unauthorized source was cloned %d times", clones)
	}
}

func TestWorkspacePathIsolationAndTraversalDefense(t *testing.T) {
	root := t.TempDir()
	first, err := workspacePath(root, "workspace-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := workspacePath(root, "workspace-b")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("different Workspace IDs resolved to same path %q", first)
	}
	for _, bad := range []string{"../escape", "nested/workspace", `nested\\workspace`} {
		if _, err := workspacePath(root, bad); !errors.Is(err, ErrInvalidMetadata) {
			t.Fatalf("workspacePath(%q) error = %v, want ErrInvalidMetadata", bad, err)
		}
	}
}

func TestValidateIdentityRejectsCrossProjectWorkspace(t *testing.T) {
	project := store.Project{ID: "project-a", RepositoryPath: "/repo", DefaultBranch: "main"}
	issue := store.Issue{ID: "issue-a", ProjectID: project.ID}
	workspace := store.Workspace{ID: "workspace-a", ProjectID: "project-b", IssueID: issue.ID, WorkingBranch: "agent-board/issue-a"}
	if err := validateIdentity(project, issue, workspace); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("validateIdentity() error = %v, want ErrInvalidMetadata", err)
	}
}
