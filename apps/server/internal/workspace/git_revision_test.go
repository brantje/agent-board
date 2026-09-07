package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestFullCommitRevisionValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		revision string
		want     bool
	}{
		"sha1 lower":     {revision: strings.Repeat("a", 40), want: true},
		"sha256 upper":   {revision: strings.Repeat("A", 64), want: true},
		"wrong length":   {revision: strings.Repeat("a", 39), want: false},
		"non hexadecimal": {revision: strings.Repeat("g", 40), want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := isFullCommitRevision(tc.revision); got != tc.want {
				t.Fatalf("isFullCommitRevision(%q)=%v want %v", tc.revision, got, tc.want)
			}
		})
	}
}

func TestCheckoutRevisionRejectsUntrustedRevisionBeforeGitExecution(t *testing.T) {
	git := &GitCLI{}
	if err := git.CheckoutRevision(context.Background(), t.TempDir(), "--detach HEAD"); err == nil {
		t.Fatal("CheckoutRevision() accepted a non-commit revision")
	}
}

func TestMaterializerRequiresExactRevisionCheckoutForPinnedWorkspace(t *testing.T) {
	baseGit := requireGit(t)
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "sources")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := createFixtureRepository(t, baseGit.GitCLI, sourceRoot)
	revision, err := baseGit.HeadRevision(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}

	policy, err := repository.NewPolicy([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	current := fixtureWorkspace(source)
	current.BaseRevision = workspaceStringPointer(revision)
	state := &memoryStateStore{workspace: current}

	// Restrict the wrapped implementation to the core Git interface so it does
	// not expose CheckoutRevision. A pinned workspace must fail closed rather
	// than silently cloning a mutable branch tip.
	var coreGit Git = baseGit
	materializer, err := NewMaterializer(
		state,
		policy,
		gitWithoutRevisionCheckout{Git: coreGit},
		filepath.Join(parent, "workspaces"),
	)
	if err != nil {
		t.Fatal(err)
	}
	project := store.Project{ID: "project-1", RepositoryPath: source, DefaultBranch: "main"}
	issue := store.Issue{ID: "issue-1", ProjectID: project.ID}

	if _, err := materializer.Ensure(context.Background(), project, issue, current); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("Ensure() error=%v want ErrInvalidMetadata", err)
	}
}

type gitWithoutRevisionCheckout struct{ Git }
