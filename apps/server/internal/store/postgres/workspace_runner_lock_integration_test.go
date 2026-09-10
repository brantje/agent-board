package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRunnerWorkspaceOwnershipFencesGenericWriters(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "runner-workspace-lock")
	enqueueWorkspaceOwnerJob(t, s, f)
	runner, err := s.CreateRunner(ctx, store.Runner{Name: "runner-workspace-lock", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}
	session, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID,
		RunID:     f.run.ID,
		RunnerID:  runner.ID,
		Status:    "PENDING",
	})
	if err != nil {
		t.Fatalf("create runner session: %v", err)
	}

	if lock, err := s.AcquireWorkspaceBootstrapLock(ctx, f.workspace.ID); !errors.Is(err, store.ErrConflict) {
		if lock != nil {
			_ = lock.Release()
		}
		t.Fatalf("generic writer error=%v, want ErrConflict", err)
	}
	owned, err := s.AcquireWorkspaceExecutionLock(ctx, f.workspace.ID, session.ID)
	if err != nil {
		t.Fatalf("owning runner lock: %v", err)
	}
	if err := owned.Release(); err != nil {
		t.Fatalf("release owning runner lock: %v", err)
	}
	if lock, err := s.AcquireWorkspaceExecutionLock(ctx, f.workspace.ID, "00000000-0000-0000-0000-000000000001"); !errors.Is(err, store.ErrConflict) {
		if lock != nil {
			_ = lock.Release()
		}
		t.Fatalf("wrong runner owner error=%v, want ErrConflict", err)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE scheduler_jobs SET state='DONE', updated_at=now() WHERE run_id=$1`, f.run.ID); err != nil {
		t.Fatalf("finish scheduler ownership: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='WAITING_FOR_INPUT', updated_at=now() WHERE id=$1`, f.run.ID); err != nil {
		t.Fatalf("enter waiting state: %v", err)
	}
	if lock, err := s.AcquireWorkspaceBootstrapLock(ctx, f.workspace.ID); !errors.Is(err, store.ErrConflict) {
		if lock != nil {
			_ = lock.Release()
		}
		t.Fatalf("waiting generic writer error=%v, want ErrConflict", err)
	}
	waitingOwned, err := s.AcquireWorkspaceExecutionLock(ctx, f.workspace.ID, session.ID)
	if err != nil {
		t.Fatalf("waiting owning runner lock: %v", err)
	}
	if err := waitingOwned.Release(); err != nil {
		t.Fatalf("release waiting owning runner lock: %v", err)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE runs SET status='READY_FOR_REVIEW', updated_at=now() WHERE id=$1`, f.run.ID); err != nil {
		t.Fatalf("release waiting ownership: %v", err)
	}
	generic, err := s.AcquireWorkspaceBootstrapLock(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("generic writer after ownership release: %v", err)
	}
	if err := generic.Release(); err != nil {
		t.Fatalf("release generic writer: %v", err)
	}
}

func TestWorkspaceWriterAdmissionCannotRaceGenericMutation(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "runner-workspace-admission")
	enqueueWorkspaceOwnerJob(t, s, f)
	runner, err := s.CreateRunner(ctx, store.Runner{Name: "runner-workspace-admission", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	generic, err := s.AcquireWorkspaceBootstrapLock(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("acquire generic writer: %v", err)
	}
	created := make(chan error, 1)
	go func() {
		_, createErr := s.CreateExecutionSession(context.Background(), store.ExecutionSession{
			ProjectID: f.project.ID,
			RunID:     f.run.ID,
			RunnerID:  runner.ID,
			Status:    "PENDING",
		})
		created <- createErr
	}()

	select {
	case err := <-created:
		_ = generic.Release()
		t.Fatalf("runner ownership crossed active generic mutation lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := generic.Release(); err != nil {
		t.Fatalf("release generic writer: %v", err)
	}
	select {
	case err := <-created:
		if err != nil {
			t.Fatalf("create runner owner after generic mutation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner ownership did not proceed after generic mutation released")
	}
}

func enqueueWorkspaceOwnerJob(t *testing.T, s *Store, f runFixture) {
	t.Helper()
	if _, err := s.EnqueueJob(context.Background(), store.SchedulerJob{
		ProjectID:      f.project.ID,
		RunID:          f.run.ID,
		Kind:           "START",
		IdempotencyKey: "workspace-owner-" + f.run.ID,
	}); err != nil {
		t.Fatalf("enqueue workspace owner job: %v", err)
	}
}
