package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSchedulerRejectsEnqueueWithoutProfileChain(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "invalid-enqueue")
	run := createRunWithoutAgent(t, s, f, "invalid-enqueue")

	_, err := s.EnqueueJob(context.Background(), store.SchedulerJob{
		ProjectID:      f.project.ID,
		RunID:          run.ID,
		IdempotencyKey: "invalid-enqueue",
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("enqueue error=%v want conflict", err)
	}

	var jobs int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM scheduler_jobs WHERE run_id=$1`, run.ID).Scan(&jobs); err != nil {
		t.Fatalf("count scheduler jobs: %v", err)
	}
	if jobs != 0 {
		t.Fatalf("scheduler jobs=%d want 0", jobs)
	}
}

func TestSchedulerAdmissionDefersPersistedInvalidProfileChain(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "invalid-admission")
	run := createRunWithoutAgent(t, s, f, "invalid-admission")
	jobID := insertRawSchedulerJob(t, s, f.project.ID, run.ID, "invalid-admission", "QUEUED")

	admission, err := s.AdmitNextJob(ctx, "worker", time.Minute, time.Second)
	if err != nil {
		t.Fatalf("admit invalid profile chain: %v", err)
	}
	if admission != nil {
		t.Fatalf("admission=%+v want configuration wait", admission)
	}
	assertConfigurationWait(t, s, jobID, run.ID)
	assertSchedulerOwnershipCounts(t, s, jobID, 0, 0)
}

func TestLegacyClaimDefersInvalidProfileChainWithoutLease(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "invalid-legacy-claim")
	run := createRunWithoutAgent(t, s, f, "invalid-legacy-claim")
	jobID := insertRawSchedulerJob(t, s, f.project.ID, run.ID, "invalid-legacy-claim", "QUEUED")

	job, lease, err := s.ClaimNextJob(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatalf("legacy claim invalid profile chain: %v", err)
	}
	if job != nil || lease != nil {
		t.Fatalf("job=%+v lease=%+v want no claim", job, lease)
	}
	assertConfigurationWait(t, s, jobID, run.ID)
	assertSchedulerOwnershipCounts(t, s, jobID, 0, 0)
}

func TestReconciliationRecoversExpiredInvalidLegacyClaim(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "invalid-reconciliation")
	run := createRunWithoutAgent(t, s, f, "invalid-reconciliation")
	jobID := insertRawSchedulerJob(t, s, f.project.ID, run.ID, "invalid-reconciliation", "CLAIMED")
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO scheduler_leases (job_id, owner_id, acquired_at, expires_at)
		VALUES ($1, 'old-worker', now() - interval '2 minutes', now() - interval '1 minute')
	`, jobID); err != nil {
		t.Fatalf("insert expired lease: %v", err)
	}

	claim, err := s.ClaimExpiredJobForReconciliation(ctx, "reconciler", time.Minute)
	if err != nil {
		t.Fatalf("claim invalid reconciliation: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim=%+v want invalid queued claim reset", claim)
	}
	assertConfigurationWait(t, s, jobID, run.ID)
	assertSchedulerOwnershipCounts(t, s, jobID, 0, 0)
}

func createRunWithoutAgent(t *testing.T, s *Store, f runFixture, suffix string) store.Run {
	t.Helper()
	ctx := context.Background()
	issue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID,
		Title:     "issue " + suffix,
		Status:    "IN_PROGRESS",
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
		Attempt:     1,
	})
	if err != nil {
		t.Fatalf("create run %s: %v", suffix, err)
	}
	return run
}

func insertRawSchedulerJob(t *testing.T, s *Store, projectID, runID, key, state string) string {
	t.Helper()
	var jobID string
	if err := s.pool.QueryRow(context.Background(), `
		INSERT INTO scheduler_jobs (project_id, run_id, kind, state, idempotency_key)
		VALUES ($1, $2, 'START', $3, $4)
		RETURNING id::text
	`, projectID, runID, state, key).Scan(&jobID); err != nil {
		t.Fatalf("insert raw scheduler job %s: %v", key, err)
	}
	return jobID
}

func assertConfigurationWait(t *testing.T, s *Store, jobID, runID string) {
	t.Helper()
	var state string
	var jobReason, runReason *string
	if err := s.pool.QueryRow(context.Background(), `
		SELECT job.state, job.wait_reason, run.queue_reason
		FROM scheduler_jobs AS job
		JOIN runs AS run ON run.id=job.run_id
		WHERE job.id=$1 AND run.id=$2
	`, jobID, runID).Scan(&state, &jobReason, &runReason); err != nil {
		t.Fatalf("read configuration wait: %v", err)
	}
	if state != "QUEUED" {
		t.Fatalf("job state=%s want QUEUED", state)
	}
	if jobReason == nil || *jobReason != schedulerConfigurationWaitReason {
		t.Fatalf("job wait reason=%v want %q", jobReason, schedulerConfigurationWaitReason)
	}
	if runReason == nil || *runReason != schedulerConfigurationWaitReason {
		t.Fatalf("run queue reason=%v want %q", runReason, schedulerConfigurationWaitReason)
	}
}
