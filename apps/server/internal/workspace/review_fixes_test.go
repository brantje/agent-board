package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type branchInspectionFailureGit struct {
	*GitCLI
	err error
}

func (g *branchInspectionFailureGit) CurrentBranch(context.Context, string) (string, error) {
	return "", g.err
}

type failureContextStore struct {
	memoryStateStore
	failureContextErr error
}

func (s *failureContextStore) MarkWorkspaceBootstrapFailed(ctx context.Context, projectID, issueID, workspaceID string) (store.Workspace, error) {
	s.failureContextErr = ctx.Err()
	return s.memoryStateStore.MarkWorkspaceBootstrapFailed(ctx, projectID, issueID, workspaceID)
}

func TestGitCLIIsRepositoryRequiresExactRoot(t *testing.T) {
	git := requireGit(t).GitCLI
	root := t.TempDir()
	source := createFixtureRepository(t, git, root)
	nested := filepath.Join(source, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	isRepository, err := git.IsRepository(context.Background(), source)
	if err != nil || !isRepository {
		t.Fatalf("source repository check = %v, err=%v", isRepository, err)
	}
	isRepository, err = git.IsRepository(context.Background(), nested)
	if err != nil {
		t.Fatalf("nested repository check error = %v", err)
	}
	if isRepository {
		t.Fatal("nested directory inside a parent repository was accepted as a repository root")
	}
}

func TestGitCLIRunKeepsSuccessfulStderrOutOfData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script Git stand-in is Unix-only")
	}
	binary := writeExecutable(t, "#!/bin/sh\nprintf 'revision-data\\n'\nprintf 'warning on stderr\\n' >&2\n")
	git, err := NewGitCLIWithTimeout(binary, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	output, err := git.run(context.Background(), "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if output != "revision-data" {
		t.Fatalf("run() output = %q, want clean stdout", output)
	}
}

func TestGitCLIAppliesCommandTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script Git stand-in is Unix-only")
	}
	binary := writeExecutable(t, "#!/bin/sh\nexec sleep 5\n")
	git, err := NewGitCLIWithTimeout(binary, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.run(context.Background(), "status"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run() error = %v, want context deadline exceeded", err)
	}
	if _, err := NewGitCLIWithTimeout(binary, 0); err == nil {
		t.Fatal("zero Git command timeout unexpectedly accepted")
	}
}

func TestMaterializerReadyWorkspaceFailsIfCheckoutDisappears(t *testing.T) {
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

	ready, err := materializer.Ensure(context.Background(), project, issue, state.workspace)
	if err != nil {
		t.Fatalf("first Ensure() error = %v", err)
	}
	if err := os.RemoveAll(ready.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Ensure(context.Background(), project, issue, ready); !errors.Is(err, ErrBootstrapFailed) {
		t.Fatalf("Ensure() after checkout loss error = %v, want ErrBootstrapFailed", err)
	}
	if clones := git.clones.Load(); clones != 1 {
		t.Fatalf("checkout loss triggered %d clones, want no destructive re-bootstrap", clones)
	}
}

func TestMaterializerRecoveryInspectionErrorPreservesPublishedCheckout(t *testing.T) {
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
	workspaceRoot := filepath.Join(parent, "workspaces")
	materializer, err := NewMaterializer(state, policy, git, workspaceRoot)
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
	preserved := filepath.Join(current.Path, "preserved.txt")
	if err := os.WriteFile(preserved, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	transient := errors.New("temporary Git inspection failure")
	failingGit := &branchInspectionFailureGit{GitCLI: git.GitCLI, err: transient}
	materializer, err = NewMaterializer(state, policy, failingGit, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Ensure(context.Background(), project, issue, current); !errors.Is(err, transient) {
		t.Fatalf("recovery error = %v, want transient inspection error", err)
	}
	content, err := os.ReadFile(preserved)
	if err != nil || string(content) != "keep me\n" {
		t.Fatalf("published checkout was modified after inspection failure: content=%q err=%v", content, err)
	}
}

func TestMaterializerFailPreservesConfigurationClassificationAndDetachedStateWrite(t *testing.T) {
	state := &failureContextStore{memoryStateStore: memoryStateStore{workspace: fixtureWorkspace("/repository")}}
	materializer := &Materializer{store: state}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, classification := range []error{ErrInvalidMetadata, ErrInvalidRoot} {
		cause := fmt.Errorf("configuration problem: %w", classification)
		_, err := materializer.fail(ctx, state.workspace, cause)
		if !errors.Is(err, classification) {
			t.Fatalf("fail() error = %v, want %v", err, classification)
		}
		if errors.Is(err, ErrBootstrapFailed) {
			t.Fatalf("configuration error was reclassified as bootstrap failure: %v", err)
		}
	}
	if state.failureContextErr != nil {
		t.Fatalf("FAILED transition inherited canceled context: %v", state.failureContextErr)
	}
	if !state.failed {
		t.Fatal("FAILED transition was not attempted")
	}
}

func writeExecutable(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "git-stand-in")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateReadyCheckoutRejectsNonRepositoryDirectory(t *testing.T) {
	git := requireGit(t)
	materializer := &Materializer{git: git}
	path := t.TempDir()
	err := materializer.validateReadyCheckout(context.Background(), store.Workspace{Path: path})
	if !errors.Is(err, ErrBootstrapFailed) || !strings.Contains(err.Error(), "not a Git repository") {
		t.Fatalf("validateReadyCheckout() error = %v", err)
	}
}

func TestMaterializerReadyWorkspaceRejectsForeignRepository(t *testing.T) {
	git := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, git.GitCLI, sourceRoot)
	foreignRoot := filepath.Join(parent, "foreign-sources")
	if err := os.Mkdir(foreignRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	foreignSource := createFixtureRepository(t, git.GitCLI, foreignRoot)
	policy, err := repository.NewPolicy([]string{sourceRoot, foreignRoot})
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

	ready, err := materializer.Ensure(context.Background(), project, issue, state.workspace)
	if err != nil {
		t.Fatalf("first Ensure() error = %v", err)
	}
	if err := os.RemoveAll(ready.Path); err != nil {
		t.Fatal(err)
	}
	if err := git.GitCLI.Clone(context.Background(), foreignSource, ready.Path, "main"); err != nil {
		t.Fatal(err)
	}
	if err := git.GitCLI.CheckoutNewBranch(context.Background(), ready.Path, ready.WorkingBranch); err != nil {
		t.Fatal(err)
	}

	if _, err := materializer.Ensure(context.Background(), project, issue, ready); !errors.Is(err, ErrBootstrapFailed) || !strings.Contains(err.Error(), "origin does not match") {
		t.Fatalf("Ensure() with foreign READY checkout error = %v, want origin identity failure", err)
	}
}
