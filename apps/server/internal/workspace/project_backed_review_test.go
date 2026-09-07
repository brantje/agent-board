package workspace

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reviewProjectBackedStore struct {
	*memoryStateStore
	mu sync.Mutex
}

func (s *reviewProjectBackedStore) AcquireWorkspaceBootstrapLock(context.Context, string) (store.WorkspaceBootstrapLock, error) {
	s.mu.Lock()
	return memoryLock{mu: &s.mu}, nil
}

func TestProjectBackedMaterializerAppliesReviewedCandidate(t *testing.T) {
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
	projectMaterializer, err := NewProjectMaterializer(&projectWorkspaceLockStore{}, policy, git, filepath.Join(parent, "project-workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	accepted, err := projectMaterializer.EnsureProjectWorkspace(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}

	state := &reviewProjectBackedStore{memoryStateStore: &memoryStateStore{workspace: fixtureWorkspace(source)}}
	legacy, err := NewMaterializer(state, policy, git.GitCLI, filepath.Join(parent, "issues"))
	if err != nil {
		t.Fatal(err)
	}
	backed, err := NewProjectBackedMaterializer(legacy, staticProjectWorkspaceSource{value: accepted})
	if err != nil {
		t.Fatal(err)
	}
	candidate := AcceptedCandidate{Files: []CandidateFileSource{{Path: "reviewed.txt", Chunks: []CandidateBlobSource{func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("reviewed\n")), nil
	}}}}}
	revision, err := backed.ApplyReviewedCandidate(t.Context(), project, "review-backed", candidate)
	if err != nil {
		t.Fatalf("ApplyReviewedCandidate() error=%v", err)
	}
	if revision == accepted.AcceptedRevision {
		t.Fatalf("accepted revision did not advance: %q", revision)
	}
	content, err := os.ReadFile(filepath.Join(accepted.Path, "reviewed.txt"))
	if err != nil || string(content) != "reviewed\n" {
		t.Fatalf("reviewed content=%q err=%v", content, err)
	}
}

func TestProjectBackedMaterializerReviewDeliveryValidatesDependencies(t *testing.T) {
	var nilMaterializer *ProjectBackedMaterializer
	if _, err := nilMaterializer.ApplyReviewedCandidate(t.Context(), store.Project{}, "review", AcceptedCandidate{}); err == nil {
		t.Fatal("nil materializer should fail")
	}

	git := requireGit(t)
	policy, _ := repository.NewPolicy([]string{t.TempDir()})
	state := &memoryStateStore{}
	legacy, err := NewMaterializer(state, policy, git, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backed, err := NewProjectBackedMaterializer(legacy, staticProjectWorkspaceSource{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backed.ApplyReviewedCandidate(t.Context(), store.Project{}, "review", AcceptedCandidate{}); err == nil {
		t.Fatal("materializer without Project Workspace locking should fail")
	}
}
