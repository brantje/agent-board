package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectBackedReviewKeepsTwoIssueBranchesIndependentAcrossTargetAdvance(t *testing.T) {
	issueA := newReviewedRevisionFixture(t)

	issueBPath := filepath.Join(filepath.Dir(issueA.issuePath), "issue-b-workspace")
	if err := issueA.git.Clone(t.Context(), issueA.project.RepositoryPath, issueBPath, "main"); err != nil {
		t.Fatal(err)
	}
	issueBBase, err := issueA.git.HeadRevision(t.Context(), issueBPath)
	if err != nil {
		t.Fatal(err)
	}
	if issueBBase != issueA.baseRevision {
		t.Fatalf("Issue B base=%q want shared target %q", issueBBase, issueA.baseRevision)
	}
	issueBBranch := "agent-board/AB-3"
	if err := issueA.git.CheckoutNewBranch(t.Context(), issueBPath, issueBBranch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueBPath, "reviewed.txt"), []byte("issue B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := issueA.git.run(t.Context(), "-C", issueBPath, "add", "--", "reviewed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := issueA.git.run(t.Context(), "-C", issueBPath, "-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost", "commit", "-m", "Issue B reviewed change"); err != nil {
		t.Fatal(err)
	}
	issueBRevision, err := issueA.git.HeadRevision(t.Context(), issueBPath)
	if err != nil {
		t.Fatal(err)
	}
	issueAHeadBefore, err := issueA.git.HeadRevision(t.Context(), issueA.issuePath)
	if err != nil {
		t.Fatal(err)
	}

	acceptedA, err := issueA.backed.ApplyReviewedRevision(t.Context(), issueA.project, issueA.review)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issueA.git.run(t.Context(), "-C", issueA.accepted.Path, "merge-base", "--is-ancestor", issueA.reviewRevision, acceptedA); err != nil {
		t.Fatalf("Issue A reviewed revision was not integrated: %v", err)
	}
	if got, err := issueA.git.HeadRevision(t.Context(), issueBPath); err != nil || got != issueBRevision {
		t.Fatalf("Issue A approval mutated Issue B: head=%q err=%v want=%q", got, err, issueBRevision)
	}
	if parent, err := issueA.git.run(t.Context(), "-C", issueBPath, "rev-parse", issueBRevision+"^"); err != nil || strings.TrimSpace(parent) != issueBBase {
		t.Fatalf("Issue B was reset/rebased after target advance: parent=%q err=%v want=%q", parent, err, issueBBase)
	}

	policy, err := repository.NewPolicy([]string{filepath.Dir(issueA.project.RepositoryPath)})
	if err != nil {
		t.Fatal(err)
	}
	stateB := &reviewProjectBackedStore{memoryStateStore: &memoryStateStore{workspace: store.Workspace{
		ProjectID:       issueA.project.ID,
		IssueID:         "issue-3",
		Path:            issueBPath,
		WorkingBranch:   issueBBranch,
		BootstrapStatus: "READY",
	}}}
	materializerB, err := NewMaterializer(stateB, policy, issueA.git, filepath.Join(filepath.Dir(issueA.issuePath), "issues-b"))
	if err != nil {
		t.Fatal(err)
	}
	backedB, err := NewProjectBackedMaterializer(materializerB, staticProjectWorkspaceSource{value: issueA.accepted})
	if err != nil {
		t.Fatal(err)
	}
	reviewB := store.Review{
		ID:             "review-3",
		ProjectID:      issueA.project.ID,
		IssueID:        "issue-3",
		BaseRevision:   issueBBase,
		ReviewRevision: issueBRevision,
	}
	targetAfterA, err := issueA.git.HeadRevision(t.Context(), issueA.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := backedB.ApplyReviewedRevision(t.Context(), issueA.project, reviewB); err == nil || !strings.Contains(err.Error(), "integrate reviewed revision") {
		t.Fatalf("Issue B conflicting approval error=%v", err)
	}
	if got, err := issueA.git.HeadRevision(t.Context(), issueA.accepted.Path); err != nil || got != targetAfterA {
		t.Fatalf("failed Issue B approval moved Project target: head=%q err=%v want=%q", got, err, targetAfterA)
	}
	if status, err := issueA.git.run(t.Context(), "-C", issueA.accepted.Path, "status", "--porcelain"); err != nil || strings.TrimSpace(status) != "" {
		t.Fatalf("failed Issue B approval left Project target dirty: status=%q err=%v", status, err)
	}
	if got, err := issueA.git.HeadRevision(t.Context(), issueBPath); err != nil || got != issueBRevision {
		t.Fatalf("failed approval mutated Issue B branch: head=%q err=%v want=%q", got, err, issueBRevision)
	}
	if got, err := issueA.git.HeadRevision(t.Context(), issueA.issuePath); err != nil || got != issueAHeadBefore {
		t.Fatalf("Issue B approval mutated Issue A branch: head=%q err=%v want=%q", got, err, issueAHeadBefore)
	}
}
