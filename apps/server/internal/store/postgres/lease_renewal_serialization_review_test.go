package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRenewLeaseWaitsForRunLock(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "renew-run-lock")
	job, err := s.EnqueueJob(t.Context(), store.SchedulerJob{
		ProjectID:      f.project.ID,
		RunID:          f.run.ID,
		IdempotencyKey: "renew-run-lock",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, lease, err := s.ClaimNextJob(t.Context(), "worker", time.Minute)
	if err != nil || claimed == nil || lease == nil || claimed.ID != job.ID {
		t.Fatalf("claim job=%+v lease=%+v err=%v", claimed, lease, err)
	}

	lockTx, err := s.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	defer func() { _ = lockTx.Rollback(context.Background()) }()
	var runID string
	if err := lockTx.QueryRow(t.Context(), `
		SELECT id::text FROM runs WHERE project_id=$1 AND id=$2 FOR UPDATE
	`, f.project.ID, f.run.ID).Scan(&runID); err != nil {
		t.Fatalf("lock run: %v", err)
	}

	type renewalResult struct {
		lease store.SchedulerLease
		err   error
	}
	result := make(chan renewalResult, 1)
	go func() {
		renewed, renewErr := s.RenewLease(t.Context(), f.project.ID, job.ID, lease.LeaseToken, 2*time.Minute)
		result <- renewalResult{lease: renewed, err: renewErr}
	}()

	select {
	case got := <-result:
		t.Fatalf("RenewLease completed while Run row was locked: lease=%+v err=%v", got.lease, got.err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := lockTx.Commit(t.Context()); err != nil {
		t.Fatalf("release run lock: %v", err)
	}
	select {
	case got := <-result:
		if got.err != nil || !got.lease.ExpiresAt.After(lease.ExpiresAt) {
			t.Fatalf("renew after run unlock lease=%+v err=%v", got.lease, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("RenewLease did not complete after Run row lock released")
	}
}
