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

func TestMaterializerPreservesPublishedCheckoutWithUnexpectedIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *countingGit, string, string)
	}{
		{
			name: "working branch",
			mutate: func(t *testing.T, git *countingGit, workspacePath, _ string) {
				t.Helper()
				runGit(t, git.binary, "-C", workspacePath, "checkout", "-b", "engine-changed")
			},
		},
		{
			name: "origin",
			mutate: func(t *testing.T, git *countingGit, workspacePath, parent string) {
				t.Helper()
				foreignRoot := filepath.Join(parent, "foreign")
				if err := os.Mkdir(foreignRoot, 0o755); err != nil {
					t.Fatal(err)
				}
				foreign := createFixtureRepository(t, git.GitCLI, foreignRoot)
				runGit(t, git.binary, "-C", workspacePath, "remote", "set-url", "origin", foreign)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			state := &memoryStateStore{workspace: fixtureWorkspace(source), readyErr: errors.New("database unavailable")}
			materializer, err := NewMaterializer(state, policy, git, filepath.Join(parent, "workspaces"))
			if err != nil {
				t.Fatal(err)
			}
			project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
			issue := store.Issue{ID: "issue-1", ProjectID: project.ID}

			if _, err := materializer.Ensure(context.Background(), project, issue, state.workspace); err == nil {
				t.Fatal("first Ensure() unexpectedly succeeded")
			}
			current, err := state.GetWorkspaceByIssue(context.Background(), project.ID, issue.ID)
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(current.Path, "preserve-me.txt")
			if err := os.WriteFile(marker, []byte("preserve\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			tt.mutate(t, git, current.Path, parent)

			_, err = materializer.Ensure(context.Background(), project, issue, current)
			if !errors.Is(err, ErrBootstrapFailed) {
				t.Fatalf("Ensure() error = %v, want ErrBootstrapFailed", err)
			}
			content, readErr := os.ReadFile(marker)
			if readErr != nil || string(content) != "preserve\n" {
				t.Fatalf("published workspace state was removed: content=%q err=%v", content, readErr)
			}
			if clones := git.clones.Load(); clones != 1 {
				t.Fatalf("clone count = %d, want 1", clones)
			}
		})
	}
}
