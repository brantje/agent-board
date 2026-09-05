package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUpdateIssueRejectsDoneWhileRunOrSchedulerJobIsActive(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "done-guard")

	issue := f.issue
	issue.Status = "IN_PROGRESS"
	var err error
	issue, err = s.UpdateIssue(ctx, issue)
	if err != nil {
		t.Fatalf("mark issue in progress: %v", err)
	}
	if _, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID:      f.project.ID,
		RunID:          f.run.ID,
		Kind:           "START",
		IdempotencyKey: "done-guard-start",
	}); err != nil {
		t.Fatalf("enqueue start job: %v", err)
	}

	issue.Status = "DONE"
	if _, err := s.UpdateIssue(ctx, issue); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("done with queued run error=%v, want ErrConflict", err)
	}
	persisted, err := s.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatalf("get issue after rejected update: %v", err)
	}
	if persisted.Status != "IN_PROGRESS" {
		t.Fatalf("status after rejected update=%s, want IN_PROGRESS", persisted.Status)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='CANCELLED', completed_at=now(), updated_at=now() WHERE id=$1`, f.run.ID); err != nil {
		t.Fatalf("cancel run: %v", err)
	}
	if _, err := s.UpdateIssue(ctx, issue); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("done with queued scheduler job error=%v, want ErrConflict", err)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE scheduler_jobs SET state='CANCELLED', updated_at=now() WHERE run_id=$1`, f.run.ID); err != nil {
		t.Fatalf("cancel scheduler job: %v", err)
	}
	updated, err := s.UpdateIssue(ctx, issue)
	if err != nil {
		t.Fatalf("mark issue done after work is terminal: %v", err)
	}
	if updated.Status != "DONE" {
		t.Fatalf("status=%s, want DONE", updated.Status)
	}
}

func TestUpdateRuntimeNormalizesNilAllowedSecretRefs(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	project, err := s.CreateProject(ctx, store.Project{Name: "runtime-update", RepositoryPath: "/repo/runtime-update"})
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
