package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyReviewedRevisionIntegratesCleanTarget(t *testing.T) {
	fixture := newReviewedRevisionFixture(t)

	revision, err := fixture.backed.ApplyReviewedRevision(t.Context(), fixture.project, fixture.review)
	if err != nil {
		t.Fatal(err)
	}
	if revision == fixture.baseRevision {
		t.Fatal("clean target did not advance")
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "merge-base", "--is-ancestor", fixture.reviewRevision, revision); err != nil {
		t.Fatalf("delivered target does not contain reviewed revision: %v", err)
	}
	if status, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "status", "--porcelain"); err != nil || strings.TrimSpace(status) != "" {
		t.Fatalf("clean delivery status=%q err=%v", status, err)
	}
}

func TestApplyReviewedRevisionRollsBackConflictingTargetIntegration(t *testing.T) {
	fixture := newReviewedRevisionFixture(t)
	conflictingPath := filepath.Join(fixture.accepted.Path, "reviewed.txt")
	if err := os.WriteFile(conflictingPath, []byte("target version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "add", "--", "reviewed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.git.run(t.Context(), "-C", fixture.accepted.Path,
		"-c", "user.name=Target", "-c", "user.email=target@example.invalid",
		"commit", "-m", "Conflicting target change"); err != nil {
		t.Fatal(err)
	}
	targetHead, err := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = fixture.backed.ApplyReviewedRevision(t.Context(), fixture.project, fixture.review)
	if err == nil || !strings.Contains(err.Error(), "integrate reviewed revision") {
		t.Fatalf("conflicting delivery error=%v", err)
	}
	if head, headErr := fixture.git.HeadRevision(t.Context(), fixture.accepted.Path); headErr != nil || head != targetHead {
		t.Fatalf("conflicting delivery moved target: head=%q err=%v want=%q", head, headErr, targetHead)
	}
	if branch, branchErr := fixture.git.CurrentBranch(t.Context(), fixture.accepted.Path); branchErr != nil || branch != "main" {
		t.Fatalf("conflicting delivery branch=%q err=%v", branch, branchErr)
	}
	if status, statusErr := fixture.git.run(t.Context(), "-C", fixture.accepted.Path, "status", "--porcelain"); statusErr != nil || strings.TrimSpace(status) != "" {
		t.Fatalf("conflicting delivery left dirty target: status=%q err=%v", status, statusErr)
	}
	body, readErr := os.ReadFile(conflictingPath)
	if readErr != nil || string(body) != "target version\n" {
		t.Fatalf("conflicting delivery changed target content=%q err=%v", body, readErr)
	}
}
