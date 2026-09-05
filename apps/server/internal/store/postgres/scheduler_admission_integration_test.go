package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

var _ store.SchedulerStore = (*Store)(nil)

func TestSchedulerAdmissionEnforcesCombinedCapacityAtomically(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "admission-atomic")
	setSchedulerLimits(t, s, f.agent.ID, 1, f.model.ID, intPtr(1))

	first := enqueueFixtureRun(t, s, f, f.run, "atomic-first")
	secondRun := createQueuedFixtureRun(t, s, f, "atomic-second")
	second := enqueueFixtureRun(t, s, f, secondRun, "atomic-second")

	admissions := make(chan *store.SchedulerAdmission, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			admission, err := s.AdmitNextJob(ctx, "worker-"+string(rune('a'+i)), time.Minute, time.Second)
			if err != nil {
				errs <- err
				return
			}
			admissions <- admission
		}(i)
	}
	wg.Wait()
	close(admissions)
	close(errs)
	for err := range errs {
		t.Fatalf("admit concurrently: %v", err)
	}

	claimed := 0
	var admittedJobID string
	for admission := range admissions {
		if admission != nil {
			claimed++
			admittedJobID = admission.Job.ID
			if admission.Job.ID != first.ID && admission.Job.ID != second.ID {
				t.Fatalf("unexpected admitted job %s", admission.Job.ID)
			}
			if admission.Run.Status != "STARTING" {
				t.Fatalf("admitted run status=%s want STARTING", admission.Run.Status)
			}
		}
	}
	if claimed != 1 {
		t.Fatalf("admissions=%d want 1", claimed)
	}

	var reservations int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_capacity_reservations WHERE job_id=$1`, admittedJobID).Scan(&reservations); err != nil {
		t.Fatalf("count reservations: %v", err)
	}
	if reservations != 2 {
		t.Fatalf("reservations=%d want 2", reservations)
	}

	var queuedReason *string
	if err := s.pool.QueryRow(ctx, `
		SELECT run.queue_reason
		FROM scheduler_jobs AS job
		JOIN runs AS run ON run.id=job.run_id
		WHERE job.id <> $1 AND job.id IN ($2, $3)
	`, admittedJobID, first.ID, second.ID).Scan(&queuedReason); err != nil {
		t.Fatalf("read queued reason: %v", err)
	}
	if queuedReason == nil || *queuedReason != store.SchedulerWaitAgentCapacity {
		t.Fatalf("queue reason=%v want %q", queuedReason, store.SchedulerWaitAgentCapacity)
	}
}

func TestSchedulerAdmissionEnforcesModelCapacitySeparately(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "admission-model")
	setSchedulerLimits(t, s, f.agent.ID, 2, f.model.ID, intPtr(1))

	enqueueFixtureRun(t, s, f, f.run, "model-first")
	secondRun := createQueuedFixtureRun(t, s, f, "model-second")
	enqueueFixtureRun(t, s, f, secondRun, "model-second")

	first, err := s.AdmitNextJob(ctx, "worker-a", time.Minute, time.Second)
	if err != nil || first == nil {
		t.Fatalf("first admission=%+v err=%v", first, err)
	}
	second, err := s.AdmitNextJob(ctx, "worker-b", time.Minute, time.Second)
	if err != nil {
		t.Fatalf("second admission: %v", err)
	}
	if second != nil {
		t.Fatalf("second admission=%+v want capacity wait", second)
	}

	var reason *string
	if err := s.pool.QueryRow(ctx, `SELECT queue_reason FROM runs WHERE id=$1`, secondRun.ID).Scan(&reason); err != nil {
		t.Fatalf("read model wait reason: %v", err)
	}
	if reason == nil || *reason != store.SchedulerWaitModelCapacity {
		t.Fatalf("queue reason=%v want %q", reason, store.SchedulerWaitModelCapacity)
	}
}

func TestSchedulerAdmissionSupportsNAndUnlimitedModelCapacity(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "admission-unlimited")
	setSchedulerLimits(t, s, f.agent.ID, 2, f.model.ID, nil)

	runs := []store.Run{f.run,
		createQueuedFixtureRun(t, s, f, "unlimited-second"),
		createQueuedFixtureRun(t, s, f, "unlimited-third"),
	}
	for i, run := range runs {
		enqueueFixtureRun(t, s, f, run, "unlimited-"+string(rune('a'+i)))
	}

	for i := 0; i < 2; i++ {
		admission, err := s.AdmitNextJob(ctx, "worker", time.Minute, time.Second)
		if err != nil || admission == nil {
			t.Fatalf("admission %d=%+v err=%v", i, admission, err)
		}
	}
	third, err := s.AdmitNextJob(ctx, "worker", time.Minute, time.Second)
	if err != nil {
		t.Fatalf("third admission: %v", err)
	}
	if third != nil {
		t.Fatalf("third admission=%+v want agent capacity wait", third)
	}

	var activeReservations int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_capacity_reservations WHERE resource_kind='MODEL_PROFILE' AND resource_id=$1`, f.model.ID).Scan(&activeReservations); err != nil {
		t.Fatalf("count model reservations: %v", err)
	}
	if activeReservations != 2 {
		t.Fatalf("model reservations=%d want 2", activeReservations)
	}
}

func createQueuedFixtureRun(t *testing.T, s *Store, f runFixture, suffix string) store.Run {
	t.Helper()
	ctx := context.Background()
	issue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID:       f.project.ID,
		Title:           "issue " + suffix,
		Status:          "IN_PROGRESS",
		AssignedAgentID: &f.agent.ID,
	})
	if err != nil {
		t.Fatalf("create issue %s: %v", suffix, err)
	}
	workspace, err := s.CreateWorkspace(ctx, store.Workspace{
		ProjectID:     f.project.ID,
		IssueID:       issue.ID,
		Path:          "/workspace/" + suffix,
		WorkingBranch: "issue/" + suffix,
	})
	if err != nil {
		t.Fatalf("create workspace %s: %v", suffix, err)
	}
	run, err := s.CreateRun(ctx, store.Run{
		ProjectID:   f.project.ID,
		IssueID:     issue.ID,
		WorkspaceID: workspace.ID,
		AgentID:     &f.agent.ID,
		Attempt:     1,
	})
	if err != nil {
		t.Fatalf("create run %s: %v", suffix, err)
	}
	return run
}

func enqueueFixtureRun(t *testing.T, s *Store, f runFixture, run store.Run, key string) store.SchedulerJob {
	t.Helper()
	job, err := s.EnqueueJob(context.Background(), store.SchedulerJob{
		ProjectID:      f.project.ID,
		RunID:          run.ID,
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("enqueue %s: %v", key, err)
	}
	return job
}

func setSchedulerLimits(t *testing.T, s *Store, agentID string, agentLimit int, modelID string, modelLimit *int) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `UPDATE agents SET concurrency_limit=$2 WHERE id=$1`, agentID, agentLimit); err != nil {
		t.Fatalf("set agent limit: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET max_concurrent=$2 WHERE id=$1`, modelID, modelLimit); err != nil {
		t.Fatalf("set model limit: %v", err)
	}
}

func intPtr(value int) *int { return &value }
