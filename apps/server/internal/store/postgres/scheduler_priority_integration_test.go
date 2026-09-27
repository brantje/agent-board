package postgres

import (
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerAdmissionUsesLiveIssuePriorityBeforeFIFO(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "priority-live")
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)

	olderLowRun := createQueuedFixtureRun(t, s, f, "priority-live-low")
	olderLowJob := enqueueFixtureRun(t, s, f, olderLowRun, "priority-live-low")
	newerHighRun := createQueuedFixtureRun(t, s, f, "priority-live-high")
	newerHighJob := enqueueFixtureRun(t, s, f, newerHighRun, "priority-live-high")

	base := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Microsecond)
	setSchedulerJobTimes(t, s, olderLowJob.ID, base, base)
	setSchedulerJobTimes(t, s, newerHighJob.ID, base.Add(time.Minute), base.Add(time.Minute))

	// Change priority after the jobs already exist to prove admission reads the
	// authoritative Issue row rather than a scheduler-job snapshot. Give the
	// lower-priority Issue the earlier board position to prove board order is
	// independent from execution admission.
	setSchedulerIssuePriorityAndBoardPosition(t, s, olderLowRun.IssueID, 1, 1)
	setSchedulerIssuePriorityAndBoardPosition(t, s, newerHighRun.IssueID, 4, 10_000)

	admission, err := s.AdmitNextJob(ctx, "priority-live-worker", time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if admission == nil || admission.Run.ID != newerHighRun.ID || admission.Job.ID != newerHighJob.ID {
		t.Fatalf("admission=%+v want higher-priority run=%s job=%s", admission, newerHighRun.ID, newerHighJob.ID)
	}
}

func TestSchedulerAdmissionKeepsEqualPriorityFIFO(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "priority-fifo")
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)

	olderRun := createQueuedFixtureRun(t, s, f, "priority-fifo-older")
	olderJob := enqueueFixtureRun(t, s, f, olderRun, "priority-fifo-older")
	newerRun := createQueuedFixtureRun(t, s, f, "priority-fifo-newer")
	newerJob := enqueueFixtureRun(t, s, f, newerRun, "priority-fifo-newer")
	setSchedulerIssuePriorityAndBoardPosition(t, s, olderRun.IssueID, 3, 50)
	setSchedulerIssuePriorityAndBoardPosition(t, s, newerRun.IssueID, 3, 10)

	availableAt := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Microsecond)
	setSchedulerJobTimes(t, s, olderJob.ID, availableAt, availableAt)
	setSchedulerJobTimes(t, s, newerJob.ID, availableAt, availableAt.Add(time.Minute))

	admission, err := s.AdmitNextJob(ctx, "priority-fifo-worker", time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if admission == nil || admission.Run.ID != olderRun.ID || admission.Job.ID != olderJob.ID {
		t.Fatalf("admission=%+v want FIFO run=%s job=%s", admission, olderRun.ID, olderJob.ID)
	}
}

func TestSchedulerAdmissionFutureHighPriorityDoesNotBlockEligibleWork(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "priority-future")
	setSchedulerLimits(t, s, f.agent.ID, 4, f.model.ID, nil)

	futureRun := createQueuedFixtureRun(t, s, f, "priority-future-high")
	futureJob := enqueueFixtureRun(t, s, f, futureRun, "priority-future-high")
	eligibleRun := createQueuedFixtureRun(t, s, f, "priority-future-low")
	eligibleJob := enqueueFixtureRun(t, s, f, eligibleRun, "priority-future-low")
	setSchedulerIssuePriorityAndBoardPosition(t, s, futureRun.IssueID, 4, 1)
	setSchedulerIssuePriorityAndBoardPosition(t, s, eligibleRun.IssueID, 1, 2)

	now := time.Now().UTC().Truncate(time.Microsecond)
	setSchedulerJobTimes(t, s, futureJob.ID, now.Add(time.Hour), now.Add(-2*time.Minute))
	setSchedulerJobTimes(t, s, eligibleJob.ID, now.Add(-time.Minute), now.Add(-time.Minute))

	admission, err := s.AdmitNextJob(ctx, "priority-future-worker", time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if admission == nil || admission.Run.ID != eligibleRun.ID || admission.Job.ID != eligibleJob.ID {
		t.Fatalf("admission=%+v want eligible lower-priority run=%s job=%s", admission, eligibleRun.ID, eligibleJob.ID)
	}

	state, availableAt := schedulerJobStateAndAvailability(t, s, futureJob.ID)
	if state != "QUEUED" {
		t.Fatalf("future high-priority job state=%s want QUEUED", state)
	}
	if !availableAt.After(time.Now()) {
		t.Fatalf("future high-priority job available_at=%s want future timestamp", availableAt)
	}
}

func TestSchedulerAdmissionCapacityDeferredPriorityWinsWhenEligibleAgain(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "priority-capacity")
	setSchedulerLimits(t, s, f.agent.ID, 1, f.model.ID, nil)

	blockerJob := enqueueFixtureRun(t, s, f, f.run, "priority-capacity-blocker")
	setSchedulerIssuePriorityAndBoardPosition(t, s, f.run.IssueID, 4, 1)
	blocker, err := s.AdmitNextJob(ctx, "priority-capacity-blocker-worker", time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if blocker == nil || blocker.Job.ID != blockerJob.ID {
		t.Fatalf("blocker admission=%+v want job=%s", blocker, blockerJob.ID)
	}

	highRun := createQueuedFixtureRun(t, s, f, "priority-capacity-high")
	highJob := enqueueFixtureRun(t, s, f, highRun, "priority-capacity-high")
	lowRun := createQueuedFixtureRun(t, s, f, "priority-capacity-low")
	lowJob := enqueueFixtureRun(t, s, f, lowRun, "priority-capacity-low")
	setSchedulerIssuePriorityAndBoardPosition(t, s, highRun.IssueID, 4, 100)
	setSchedulerIssuePriorityAndBoardPosition(t, s, lowRun.IssueID, 1, 1)

	base := time.Now().Add(-5 * time.Minute).UTC().Truncate(time.Microsecond)
	setSchedulerJobTimes(t, s, highJob.ID, base.Add(time.Minute), base.Add(time.Minute))
	setSchedulerJobTimes(t, s, lowJob.ID, base, base)

	deferred, err := s.AdmitNextJob(ctx, "priority-capacity-defer-worker", time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if deferred != nil {
		t.Fatalf("capacity-deferred admission=%+v want nil", deferred)
	}
	state, deferredUntil := schedulerJobStateAndAvailability(t, s, highJob.ID)
	if state != "QUEUED" || !deferredUntil.After(time.Now()) {
		t.Fatalf("high-priority deferred job state=%s available_at=%s", state, deferredUntil)
	}

	if _, err := s.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID:  f.project.ID,
		JobID:      blocker.Job.ID,
		RunID:      blocker.Run.ID,
		LeaseToken: blocker.Lease.LeaseToken,
		RunStatus:  "COMPLETED",
	}); err != nil {
		t.Fatal(err)
	}

	// Model the existing capacity backoff expiring without waiting a minute in
	// the test. The job remains the same durable queued job; only its existing
	// earliest-admission timestamp becomes eligible again.
	if _, err := s.pool.Exec(ctx, `
		UPDATE scheduler_jobs
		SET available_at=now() - interval '1 second', updated_at=now()
		WHERE id=$1 AND state='QUEUED'
	`, highJob.ID); err != nil {
		t.Fatal(err)
	}

	admission, err := s.AdmitNextJob(ctx, "priority-capacity-readmit-worker", time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if admission == nil || admission.Run.ID != highRun.ID || admission.Job.ID != highJob.ID {
		t.Fatalf("readmission=%+v want high-priority run=%s job=%s before low job=%s", admission, highRun.ID, highJob.ID, lowJob.ID)
	}
}

func TestSchedulerConcurrentClaimsTakeHighestPriorityEligibleJobs(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "priority-concurrent")
	setSchedulerLimits(t, s, f.agent.ID, 2, f.model.ID, nil)

	highRun := createQueuedFixtureRun(t, s, f, "priority-concurrent-high")
	highJob := enqueueFixtureRun(t, s, f, highRun, "priority-concurrent-high")
	middleRun := createQueuedFixtureRun(t, s, f, "priority-concurrent-middle")
	middleJob := enqueueFixtureRun(t, s, f, middleRun, "priority-concurrent-middle")
	lowRun := createQueuedFixtureRun(t, s, f, "priority-concurrent-low")
	lowJob := enqueueFixtureRun(t, s, f, lowRun, "priority-concurrent-low")
	setSchedulerIssuePriorityAndBoardPosition(t, s, highRun.IssueID, 4, 300)
	setSchedulerIssuePriorityAndBoardPosition(t, s, middleRun.IssueID, 3, 200)
	setSchedulerIssuePriorityAndBoardPosition(t, s, lowRun.IssueID, 1, 100)

	availableAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
	setSchedulerJobTimes(t, s, highJob.ID, availableAt, availableAt.Add(3*time.Second))
	setSchedulerJobTimes(t, s, middleJob.ID, availableAt, availableAt.Add(2*time.Second))
	setSchedulerJobTimes(t, s, lowJob.ID, availableAt, availableAt)

	results := make(chan *store.SchedulerAdmission, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, ownerID := range []string{"priority-concurrent-a", "priority-concurrent-b"} {
		wg.Add(1)
		go func(ownerID string) {
			defer wg.Done()
			admission, err := s.AdmitNextJob(ctx, ownerID, time.Minute, time.Second)
			if err != nil {
				errs <- err
				return
			}
			results <- admission
		}(ownerID)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent admission: %v", err)
	}

	got := map[string]bool{}
	for admission := range results {
		if admission == nil {
			t.Fatal("concurrent admission unexpectedly nil")
		}
		if got[admission.Job.ID] {
			t.Fatalf("job %s claimed twice", admission.Job.ID)
		}
		got[admission.Job.ID] = true
	}
	if len(got) != 2 || !got[highJob.ID] || !got[middleJob.ID] || got[lowJob.ID] {
		t.Fatalf("concurrent claimed jobs=%v want high=%s middle=%s only", got, highJob.ID, middleJob.ID)
	}

	lowState, _ := schedulerJobStateAndAvailability(t, s, lowJob.ID)
	if lowState != "QUEUED" {
		t.Fatalf("low-priority job state=%s want QUEUED", lowState)
	}
}

func setSchedulerIssuePriorityAndBoardPosition(t *testing.T, s *Store, issueID string, priority int, boardPosition int64) {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), `
		UPDATE issues
		SET priority=$2, board_position=$3, updated_at=now()
		WHERE id=$1
	`, issueID, priority, boardPosition); err != nil {
		t.Fatal(err)
	}
}

func setSchedulerJobTimes(t *testing.T, s *Store, jobID string, availableAt, createdAt time.Time) {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), `
		UPDATE scheduler_jobs
		SET available_at=$2, created_at=$3, updated_at=now()
		WHERE id=$1
	`, jobID, availableAt, createdAt); err != nil {
		t.Fatal(err)
	}
}

func schedulerJobStateAndAvailability(t *testing.T, s *Store, jobID string) (string, time.Time) {
	t.Helper()
	var state string
	var availableAt time.Time
	if err := s.pool.QueryRow(t.Context(), `
		SELECT state, available_at
		FROM scheduler_jobs
		WHERE id=$1
	`, jobID).Scan(&state, &availableAt); err != nil {
		t.Fatal(err)
	}
	return state, availableAt
}
