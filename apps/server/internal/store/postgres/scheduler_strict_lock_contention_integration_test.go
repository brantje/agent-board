package postgres

import (
	"context"
	"testing"
	"time"
)

func TestStrictOrderSkipsLockedBoardHeadForConcurrentAdmission(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "strict-locked-head")
	enableStrictOrder(t, s, f.project.ID)
	setSchedulerLimits(t, s, f.agent.ID, 2, f.model.ID, nil)

	first := enqueueFixtureRun(t, s, f, f.run, "strict-locked-first")
	secondRun := createQueuedFixtureRun(t, s, f, "strict-locked-second")
	second := enqueueFixtureRun(t, s, f, secondRun, "strict-locked-second")
	setBoardOrder(t, s, f.project.ID, f.issue.ID, secondRun.IssueID)

	lockTx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lockTx.Rollback(ctx) }()
	var lockedID string
	if err := lockTx.QueryRow(ctx, `SELECT id::text FROM scheduler_jobs WHERE id=$1 FOR UPDATE`, first.ID).Scan(&lockedID); err != nil {
		t.Fatalf("lock board head: %v", err)
	}
	if lockedID != first.ID {
		t.Fatalf("locked job=%s want=%s", lockedID, first.ID)
	}

	admission, err := s.AdmitNextJob(ctx, "strict-locked-worker", time.Minute, time.Second)
	if err != nil || admission == nil {
		t.Fatalf("admission=%+v err=%v", admission, err)
	}
	if admission.Job.ID != second.ID {
		t.Fatalf("admitted job=%s want next unlocked board job=%s", admission.Job.ID, second.ID)
	}

	var state string
	if err := lockTx.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, first.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" {
		t.Fatalf("locked board head state=%s want QUEUED", state)
	}
}
