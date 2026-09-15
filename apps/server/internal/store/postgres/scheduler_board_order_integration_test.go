package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerAdmissionUsesBoardOrderWhenStrictOrderEnabled(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "board-order-strict")
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)
	if _, err := s.pool.Exec(ctx, `UPDATE projects SET workflow_settings='{"strictOrder":true}'::jsonb WHERE id=$1`, f.project.ID); err != nil {
		t.Fatal(err)
	}

	firstJob := enqueueFixtureRun(t, s, f, f.run, "board-order-first")
	secondRun := createQueuedFixtureRun(t, s, f, "board-order-second")
	secondJob := enqueueFixtureRun(t, s, f, secondRun, "board-order-second")
	if _, err := s.pool.Exec(ctx, `
		UPDATE issues SET status='TODO', board_position=CASE id WHEN $2 THEN 1 WHEN $3 THEN 0 ELSE board_position END
		WHERE project_id=$1 AND id IN ($2,$3)
	`, f.project.ID, f.issue.ID, secondRun.IssueID); err != nil {
		t.Fatal(err)
	}

	admission, err := s.AdmitNextJob(ctx, "strict-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != secondJob.ID || admission.Job.ID == firstJob.ID {
		t.Fatalf("admitted job=%s want board-first job=%s", admission.Job.ID, secondJob.ID)
	}
}

func TestSchedulerAdmissionPreservesExistingOrderWhenStrictOrderDisabled(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "board-order-disabled")
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)
	if _, err := s.pool.Exec(ctx, `UPDATE projects SET workflow_settings='{"strictOrder":false}'::jsonb WHERE id=$1`, f.project.ID); err != nil {
		t.Fatal(err)
	}

	firstJob := enqueueFixtureRun(t, s, f, f.run, "board-disabled-first")
	secondRun := createQueuedFixtureRun(t, s, f, "board-disabled-second")
	enqueueFixtureRun(t, s, f, secondRun, "board-disabled-second")
	if _, err := s.pool.Exec(ctx, `
		UPDATE issues SET status='TODO', board_position=CASE id WHEN $2 THEN 1 WHEN $3 THEN 0 ELSE board_position END
		WHERE project_id=$1 AND id IN ($2,$3)
	`, f.project.ID, f.issue.ID, secondRun.IssueID); err != nil {
		t.Fatal(err)
	}

	admission, err := s.AdmitNextJob(ctx, "legacy-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != firstJob.ID {
		t.Fatalf("admitted job=%s want existing-order job=%s", admission.Job.ID, firstJob.ID)
	}
}

func TestSchedulerAdmissionSkipsDependencyBlockedBoardHead(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "board-order-dependency")
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)
	if _, err := s.pool.Exec(ctx, `UPDATE projects SET workflow_settings='{"strictOrder":true}'::jsonb WHERE id=$1`, f.project.ID); err != nil {
		t.Fatal(err)
	}

	blockedJob := enqueueFixtureRun(t, s, f, f.run, "blocked-head")
	nextRun := createQueuedFixtureRun(t, s, f, "unblocked-next")
	nextJob := enqueueFixtureRun(t, s, f, nextRun, "unblocked-next")
	dependency, err := s.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "dependency", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateIssueRelationship(ctx, store.IssueRelationship{
		ProjectID: f.project.ID, SourceIssueID: f.issue.ID, TargetIssueID: dependency.ID, Type: "depends_on",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE issues SET status='TODO', board_position=CASE id WHEN $2 THEN 0 WHEN $3 THEN 1 ELSE board_position END
		WHERE project_id=$1 AND id IN ($2,$3)
	`, f.project.ID, f.issue.ID, nextRun.IssueID); err != nil {
		t.Fatal(err)
	}

	admission, err := s.AdmitNextJob(ctx, "dependency-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != nextJob.ID || admission.Job.ID == blockedJob.ID {
		t.Fatalf("admitted job=%s want unblocked job=%s", admission.Job.ID, nextJob.ID)
	}
}
