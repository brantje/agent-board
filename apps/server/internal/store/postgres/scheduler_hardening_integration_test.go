package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerManyWorkersRespectCombinedCapacity(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "hardening-race")
	setSchedulerLimits(t, s, f.agent.ID, 3, f.model.ID, intPtr(2))

	runs := []store.Run{f.run}
	for i := 1; i < 8; i++ {
		runs = append(runs, createQueuedFixtureRun(t, s, f, fmt.Sprintf("hardening-race-%d", i)))
	}
	for i, run := range runs {
		enqueueFixtureRun(t, s, f, run, fmt.Sprintf("hardening-race-%d", i))
	}

	results := make(chan *store.SchedulerAdmission, 16)
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			admission, err := s.AdmitNextJob(ctx, fmt.Sprintf("worker-%d", i), time.Minute, time.Second)
			if err != nil {
				errs <- err
				return
			}
			results <- admission
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent admission: %v", err)
	}

	admitted := 0
	for admission := range results {
		if admission != nil {
			admitted++
		}
	}
	if admitted != 2 {
		t.Fatalf("admitted=%d want model-limited 2", admitted)
	}

	var agentReservations, modelReservations int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM scheduler_capacity_reservations
		WHERE resource_kind='AGENT' AND resource_id=$1
	`, f.agent.ID).Scan(&agentReservations); err != nil {
		t.Fatalf("count agent reservations: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM scheduler_capacity_reservations
		WHERE resource_kind='MODEL_PROFILE' AND resource_id=$1
	`, f.model.ID).Scan(&modelReservations); err != nil {
		t.Fatalf("count model reservations: %v", err)
	}
	if agentReservations != 2 || modelReservations != 2 {
		t.Fatalf("reservations agent=%d model=%d want 2/2", agentReservations, modelReservations)
	}
}

func TestSchedulerFreshStoreReconcilesDurableAdmission(t *testing.T) {
	pool := testPool(t)
	firstStore := New(pool)
	ctx := context.Background()
	f := seedRunFixture(t, firstStore, "hardening-restart")
	enqueueFixtureRun(t, firstStore, f, f.run, "hardening-restart")
	admission := mustAdmit(t, firstStore, "server-before-restart")
	expireLease(t, firstStore, admission.Job.ID)

	// A fresh Store has no process-local knowledge of the previous admission.
	// PostgreSQL alone is enough to recover ownership for reconciliation.
	restartedStore := New(pool)
	reconciled, err := restartedStore.ClaimExpiredJobForReconciliation(ctx, "server-after-restart", time.Minute)
	if err != nil {
		t.Fatalf("reconcile after restart: %v", err)
	}
	if reconciled == nil {
		t.Fatal("expected durable reconciliation claim")
	}
	if reconciled.Job.ID != admission.Job.ID || reconciled.Run.ID != admission.Run.ID {
		t.Fatalf("reconciled identities job=%s run=%s", reconciled.Job.ID, reconciled.Run.ID)
	}
	if reconciled.Lease.LeaseToken == admission.Lease.LeaseToken {
		t.Fatal("restart reconciliation must fence previous ownership")
	}
	assertSchedulerOwnershipCounts(t, restartedStore, admission.Job.ID, 1, 2)
}

func TestSchedulerCapacityWaitDoesNotBlockIssue(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "hardening-board-state")
	setSchedulerLimits(t, s, f.agent.ID, 1, f.model.ID, nil)
	enqueueFixtureRun(t, s, f, f.run, "hardening-board-first")
	secondRun := createQueuedFixtureRun(t, s, f, "hardening-board-second")
	enqueueFixtureRun(t, s, f, secondRun, "hardening-board-second")

	if admission := mustAdmit(t, s, "worker-first"); admission.Run.ID != f.run.ID {
		t.Fatalf("first admitted run=%s want %s", admission.Run.ID, f.run.ID)
	}
	second, err := s.AdmitNextJob(ctx, "worker-second", time.Minute, time.Second)
	if err != nil {
		t.Fatalf("capacity wait admission: %v", err)
	}
	if second != nil {
		t.Fatalf("capacity wait unexpectedly admitted %+v", second)
	}

	var issueStatus, runStatus string
	var queueReason *string
	if err := s.pool.QueryRow(ctx, `
		SELECT issue.status, run.status, run.queue_reason
		FROM runs AS run
		JOIN issues AS issue ON issue.project_id=run.project_id AND issue.id=run.issue_id
		WHERE run.project_id=$1 AND run.id=$2
	`, f.project.ID, secondRun.ID).Scan(&issueStatus, &runStatus, &queueReason); err != nil {
		t.Fatalf("read waiting state: %v", err)
	}
	if issueStatus == "BLOCKED" {
		t.Fatal("capacity-only wait must not move Issue to BLOCKED")
	}
	if runStatus != "QUEUED" || queueReason == nil || *queueReason != store.SchedulerWaitAgentCapacity {
		t.Fatalf("waiting state issue=%s run=%s reason=%v", issueStatus, runStatus, queueReason)
	}
}
