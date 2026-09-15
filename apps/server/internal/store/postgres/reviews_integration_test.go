package postgres

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func readyReviewFixture(t *testing.T, s *Store, suffix string) (runFixture, store.Review) {
	t.Helper()
	ctx := context.Background()
	f := seedRunFixture(t, s, suffix)
	enqueueFixtureRun(t, s, f, f.run, suffix)
	admission := mustAdmit(t, s, "worker-"+suffix)
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
		t.Fatalf("transition ready: %v", err)
	}
	reviews, err := s.ListReviews(ctx, f.project.ID, store.ReviewFilter{IssueID: &f.issue.ID, Statuses: []string{"PENDING"}})
	if err != nil || len(reviews) != 1 {
		t.Fatalf("ListReviews() reviews=%+v err=%v", reviews, err)
	}
	return f, reviews[0]
}

func TestReviewApprovalIntentAndCompletionAreDurableAndIdempotent(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f, review := readyReviewFixture(t, s, "approval")
	actor := "human-1"

	first, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: f.project.ID, ReviewID: review.ID, ActorID: &actor})
	if err != nil {
		t.Fatalf("BeginReviewApproval() error=%v", err)
	}
	if first.Decision.Kind != "REVIEW_APPROVAL_INTENT" || first.Decision.Outcome != "PENDING" || first.Review.DecisionID == nil {
		t.Fatalf("approval intent=%+v review=%+v", first.Decision, first.Review)
	}
	second, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: f.project.ID, ReviewID: review.ID, ActorID: &actor})
	if err != nil || second.Decision.ID != first.Decision.ID {
		t.Fatalf("idempotent BeginReviewApproval()=%+v err=%v", second, err)
	}

	completed, err := s.CompleteReviewApproval(ctx, store.CompleteReviewApprovalCommand{
		ProjectID: f.project.ID, ReviewID: review.ID, AcceptedRevision: "accepted-sha", DeliveryComplete: true,
	})
	if err != nil {
		t.Fatalf("CompleteReviewApproval() error=%v", err)
	}
	if completed.Review.Status != "APPROVED" || completed.Run.Status != "COMPLETED" || completed.Issue.Status != f.issue.Status || completed.Decision.Outcome != "APPROVED" {
		t.Fatalf("completed approval=%+v initialIssue=%+v", completed, f.issue)
	}
	retry, err := s.CompleteReviewApproval(ctx, store.CompleteReviewApprovalCommand{
		ProjectID: f.project.ID, ReviewID: review.ID, AcceptedRevision: "accepted-sha", DeliveryComplete: true,
	})
	if err != nil || retry.Decision.ID != completed.Decision.ID || retry.Issue.Status != f.issue.Status {
		t.Fatalf("idempotent completion=%+v err=%v", retry, err)
	}
}

func TestRequestReviewChangesCreatesNextAttemptWithoutChangingIssueStatus(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f, review := readyReviewFixture(t, s, "changes")
	actor := "human-2"

	result, err := s.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{
		ProjectID: f.project.ID, ReviewID: review.ID, Feedback: "Please add the missing regression test.", ActorID: &actor,
	})
	if err != nil {
		t.Fatalf("RequestReviewChanges() error=%v", err)
	}
	if result.Review.Status != "CHANGES_REQUESTED" || result.Decision.Outcome != "CHANGES_REQUESTED" {
		t.Fatalf("review decision=%+v %+v", result.Review, result.Decision)
	}
	if result.Run.Attempt != f.run.Attempt+1 || result.Run.WorkspaceID != f.run.WorkspaceID || result.Run.Status != "QUEUED" {
		t.Fatalf("follow-up Run=%+v previous=%+v", result.Run, f.run)
	}
	if result.Job.Kind != "START" || result.Job.RunID != result.Run.ID || result.Issue.Status != f.issue.Status {
		t.Fatalf("follow-up job/issue=%+v %+v initialIssue=%+v", result.Job, result.Issue, f.issue)
	}
	predecessor, err := s.GetRun(ctx, f.project.ID, f.run.ID)
	if err != nil || predecessor.Status != "COMPLETED" || predecessor.CompletedAt == nil {
		t.Fatalf("predecessor=%+v err=%v", predecessor, err)
	}
	var active int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE issue_id=$1 AND agent_id=$2 AND status=ANY($3::text[])`, f.issue.ID, f.agent.ID, activeRunStatuses).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active pair Runs=%d err=%v", active, err)
	}
	if len(result.Events) != 3 || result.Events[0].Type != "review.changes_requested" || result.Events[1].Type != "decision.recorded" || result.Events[2].Type != "run.completed" {
		t.Fatalf("RequestReviewChanges events=%+v", result.Events)
	}

	retry, err := s.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{
		ProjectID: f.project.ID, ReviewID: review.ID, Feedback: "Please add the missing regression test.", ActorID: &actor,
	})
	if err != nil || retry.Run.ID != result.Run.ID || retry.Job.ID != result.Job.ID || retry.Issue.Status != f.issue.Status {
		t.Fatalf("idempotent RequestReviewChanges()=%+v err=%v", retry, err)
	}
	if len(retry.Events) != 0 {
		t.Fatalf("idempotent RequestReviewChanges published extra events=%+v", retry.Events)
	}
}

func TestFailedApprovalIntentCanBeChanged(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f, review := readyReviewFixture(t, s, "approval-failure")
	actor := "human-3"
	if _, err := s.BeginReviewApproval(ctx, store.BeginReviewApprovalCommand{ProjectID: f.project.ID, ReviewID: review.ID, ActorID: &actor}); err != nil {
		t.Fatal(err)
	}
	failed, err := s.FailReviewApproval(ctx, store.FailReviewApprovalCommand{ProjectID: f.project.ID, ReviewID: review.ID, Reason: "candidate conflicts with newer accepted state"})
	if err != nil || failed.Status != "PENDING" {
		t.Fatalf("FailReviewApproval() review=%+v err=%v", failed, err)
	}
	if _, err := s.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{
		ProjectID: f.project.ID, ReviewID: review.ID, Feedback: "Resolve the accepted-state conflict.", ActorID: &actor,
	}); err != nil {
		t.Fatalf("request changes after failed apply: %v", err)
	}
}

func TestReviewAttemptSafetyIsScopedToReviewedAgent(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f, reviewA := readyReviewFixture(t, s, "review-pairs")

	agentB := f.agent
	agentB.ID = ""
	agentB.Name = "review-pairs-b"
	agentB, err := s.CreateAgent(ctx, agentB)
	if err != nil {
		t.Fatal(err)
	}
	runB, err := s.CreateRun(ctx, store.Run{
		ProjectID: f.project.ID, IssueID: f.issue.ID, WorkspaceID: f.workspace.ID,
		AgentID: &agentB.ID, Attempt: f.run.Attempt + 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	enqueueFixtureRun(t, s, f, runB, "review-pairs-b")
	claimB := mustAdmit(t, s, "worker-review-pairs-b")
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: claimB.Job.ID, RunID: claimB.Run.ID,
		LeaseToken: claimB.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: claimB.Job.ID, RunID: claimB.Run.ID,
		LeaseToken: claimB.Lease.LeaseToken, RunStatus: "READY_FOR_REVIEW",
	}); err != nil {
		t.Fatal(err)
	}
	reviewB, err := s.GetReviewByRun(ctx, f.project.ID, runB.ID)
	if err != nil || reviewB.Status != "PENDING" {
		t.Fatalf("agent B review=%+v err=%v", reviewB, err)
	}

	result, err := s.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{
		ProjectID: f.project.ID, ReviewID: reviewA.ID, Feedback: "Agent A follow-up",
	})
	if err != nil {
		t.Fatalf("Agent B's newer review blocked Agent A: %v", err)
	}
	if result.Run.AgentID == nil || *result.Run.AgentID != f.agent.ID || result.Run.Attempt <= runB.Attempt {
		t.Fatalf("Agent A continuation=%+v Agent B run=%+v", result.Run, runB)
	}
	reviewB, err = s.GetReview(ctx, f.project.ID, reviewB.ID)
	if err != nil || reviewB.Status != "PENDING" {
		t.Fatalf("Agent B pinned review changed=%+v err=%v", reviewB, err)
	}
}

func TestFailedReviewContinuationAllowsExplicitNextAttempt(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f, review := readyReviewFixture(t, s, "review-retry")
	result, err := s.RequestReviewChanges(ctx, store.RequestReviewChangesCommand{
		ProjectID: f.project.ID, ReviewID: review.ID, Feedback: "Retry after this follow-up",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := mustAdmit(t, s, "worker-review-retry")
	if claim.Run.ID != result.Run.ID {
		t.Fatalf("admitted=%s want continuation=%s", claim.Run.ID, result.Run.ID)
	}
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: claim.Job.ID, RunID: claim.Run.ID,
		LeaseToken: claim.Lease.LeaseToken, RunStatus: "RUNNING",
	}); err != nil {
		t.Fatal(err)
	}
	reason := "follow-up failed"
	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: f.project.ID, JobID: claim.Job.ID, RunID: claim.Run.ID,
		LeaseToken: claim.Lease.LeaseToken, RunStatus: "FAILED", FailureReason: &reason,
	}); err != nil {
		t.Fatal(err)
	}
	next, _, err := s.StartIssueRun(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatalf("StartIssueRun after failed continuation: %v", err)
	}
	if next.ID == result.Run.ID || next.Status != "QUEUED" || next.Attempt <= result.Run.Attempt {
		t.Fatalf("next explicit attempt=%+v failed continuation=%+v", next, result.Run)
	}
}
