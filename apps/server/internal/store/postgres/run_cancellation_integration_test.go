package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCancelInactiveRunCancelsQueuedSchedulerWorkAndPersistsEvent(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	result, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != "CANCELLED" || result.Event.Type != "run.cancelled" || result.Event.RunID == nil || *result.Event.RunID != f.parentRun.ID {
		t.Fatalf("cancellation result=%+v", result)
	}
	var queued, cancelled, events int
	if err := f.store.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM scheduler_jobs WHERE project_id=$1 AND run_id=$2 AND state='QUEUED'),
		  (SELECT count(*) FROM scheduler_jobs WHERE project_id=$1 AND run_id=$2 AND state='CANCELLED'),
		  (SELECT count(*) FROM events WHERE project_id=$1 AND run_id=$2 AND type='run.cancelled')
	`, f.project.ID, f.parentRun.ID).Scan(&queued, &cancelled, &events); err != nil {
		t.Fatal(err)
	}
	if queued != 0 || cancelled != 1 || events != 1 {
		t.Fatalf("queued=%d cancelled=%d events=%d", queued, cancelled, events)
	}
}

func TestCancelInactiveRunRefusesClaimedOrExecutingRun(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	jobID, _ := claimDelegationParentJob(t, f)
	if _, err := f.store.CancelInactiveRun(ctx, f.project.ID, f.parentRun.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("claimed cancellation err=%v want conflict", err)
	}
	var state string
	if err := f.store.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE id=$1`, jobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "CLAIMED" {
		t.Fatalf("claimed job state=%s", state)
	}
}
