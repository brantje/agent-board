package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerReadyForReviewCreatesPendingReviewAndProjectsIssue(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "review-ready")
	enqueueFixtureRun(t, s, f, f.run, "review-ready")
	admission := mustAdmit(t, s, "worker-review-ready")

	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatalf("transition running: %v", err)
	}
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: admission.Job.ID, RunID: f.run.ID,
		LeaseToken: admission.Lease.LeaseToken, RunStatus: "READY_FOR_REVIEW",
	}); err != nil {
		t.Fatalf("transition ready for review: %v", err)
	}

	var reviewProjectID, reviewIssueID, reviewStatus string
	if err := s.pool.QueryRow(ctx, `
		SELECT project_id::text, issue_id::text, status
		FROM reviews WHERE run_id=$1
	`, f.run.ID).Scan(&reviewProjectID, &reviewIssueID, &reviewStatus); err != nil {
		t.Fatalf("read Review: %v", err)
	}
	if reviewProjectID != f.project.ID || reviewIssueID != f.issue.ID || reviewStatus != "PENDING" {
		t.Fatalf("Review scope/status = %s/%s/%s", reviewProjectID, reviewIssueID, reviewStatus)
	}

	var issueStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM issues WHERE project_id=$1 AND id=$2`, f.project.ID, f.issue.ID).Scan(&issueStatus); err != nil {
		t.Fatalf("read Issue: %v", err)
	}
	if issueStatus != "REVIEW" {
		t.Fatalf("Issue status=%s want REVIEW", issueStatus)
	}
}
