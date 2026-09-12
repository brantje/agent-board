package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyReviewedRevisionRejectsWrongProjectTargetBranch(t *testing.T) {
	fixture := newReviewedRevisionFixture(t)
	before, err := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "checkout", "-qb", "unexpected-target"); err != nil {
		t.Fatal(err)
	}

	_, err = fixture.backed.ApplyReviewedRevision(t.Context(), fixture.project, fixture.review)
	if err == nil || !strings.Contains(err.Error(), "Project Workspace is on branch") {
		t.Fatalf("ApplyReviewedRevision() error=%v", err)
	}
	after, err := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("rejected delivery moved Project target: got=%q want=%q", after, before)
	}
}

func TestApplyReviewedRevisionRejectsDirtyProjectTarget(t *testing.T) {
	fixture := newReviewedRevisionFixture(t)
	before, err := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.accepted.Path, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = fixture.backed.ApplyReviewedRevision(t.Context(), fixture.project, fixture.review)
	if err == nil || !strings.Contains(err.Error(), "must be clean before review delivery") {
		t.Fatalf("ApplyReviewedRevision() error=%v", err)
	}
	after, err := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("rejected dirty delivery moved Project target: got=%q want=%q", after, before)
	}
}

func TestApplyReviewedRevisionRejectsIssueBranchThatDroppedPinnedReview(t *testing.T) {
	fixture := newReviewedRevisionFixture(t)
	before, err := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the pinned Review commit available in the Project repository while
	// rewriting the live Issue branch to a different descendant of the same base.
	// This isolates the branch-containment guard from object-availability checks.
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "fetch", "--no-tags", "--no-write-fetch-head", fixture.issuePath,
		fixture.reviewRevision+":refs/agent-board/test/pinned-review"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.issuePath, "reset", "--hard", fixture.baseRevision); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.issuePath, "replacement.txt"), []byte("replacement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.issuePath, "add", "--", "replacement.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.issuePath,
		"-c", "user.name=Agent Board", "-c", "user.email=agent-board@localhost",
		"commit", "-m", "Replace reviewed change"); err != nil {
		t.Fatal(err)
	}

	_, err = fixture.backed.ApplyReviewedRevision(t.Context(), fixture.project, fixture.review)
	if err == nil || !strings.Contains(err.Error(), "pinned review revision is no longer contained by the Issue branch") {
		t.Fatalf("ApplyReviewedRevision() error=%v", err)
	}
	after, err := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("rewritten Issue branch moved Project target: got=%q want=%q", after, before)
	}
}
