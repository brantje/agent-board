package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUpdateIssueDonePreservesRunAndSchedulerJob(t *testing.T) {
	s := New(testPool(t))
	ctx := t.Context()
	f := seedRunFixture(t, s, "done-ownership")
	if _, err := s.EnqueueJob(ctx, store.SchedulerJob{ProjectID: f.project.ID, RunID: f.run.ID, Kind: "START", IdempotencyKey: "start"}); err != nil {
		t.Fatal(err)
	}
	issue := f.issue
	issue.Status = "DONE"
	updated, err := s.UpdateIssue(ctx, issue)
	if err != nil || updated.Status != "DONE" {
		t.Fatalf("issue=%+v err=%v", updated, err)
	}
	run, err := s.GetRun(ctx, f.project.ID, f.run.ID)
	if err != nil || run.Status != "QUEUED" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	var state string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM scheduler_jobs WHERE run_id=$1`, f.run.ID).Scan(&state); err != nil || state != "QUEUED" {
		t.Fatalf("job=%s err=%v", state, err)
	}
}

func TestUpdateRuntimeNormalizesNilAllowedSecretRefs(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	project, err := s.CreateProject(ctx, testProjectInput("runtime-update", "/repo/runtime-update", "RTUP"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	runtime, err := s.CreateRuntime(ctx, store.Runtime{
		ProjectID:     &project.ID,
		Name:          "runtime-update",
		Kind:          "docker",
		Image:         "before",
		NetworkPolicy: "none",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	runtime.Image = "after"
	runtime.AllowedSecretRefs = nil
	updated, err := s.UpdateRuntime(ctx, &project.ID, runtime)
	if err != nil {
		t.Fatalf("update runtime with omitted allowedSecretRefs: %v", err)
	}
	if len(updated.AllowedSecretRefs) != 0 {
		t.Fatalf("allowedSecretRefs=%v, want empty", updated.AllowedSecretRefs)
	}
}

func TestNotFoundMapsNotNullViolationToInvalidArgument(t *testing.T) {
	err := notFound(&pgconn.PgError{Code: "23502"})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("error=%v, want ErrInvalidArgument", err)
	}
}
