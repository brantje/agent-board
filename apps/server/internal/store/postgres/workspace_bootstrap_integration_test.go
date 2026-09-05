package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestWorkspaceBootstrapTransitionsAreScopedAndReadyIsTerminal(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "workspace-bootstrap")
	other := seedRunFixture(t, s, "workspace-bootstrap-other")

	path := "/workspaces/" + fixture.workspace.ID
	pending, err := s.MarkWorkspaceBootstrapPending(ctx, fixture.project.ID, fixture.issue.ID, fixture.workspace.ID, path, "/repos/source", "main", fixture.workspace.WorkingBranch)
	if err != nil {
		t.Fatalf("mark pending: %v", err)
	}
	if pending.BootstrapStatus != "PENDING" || pending.RepositoryPath == nil || *pending.RepositoryPath != "/repos/source" || pending.BaseRevision != nil {
		t.Fatalf("pending workspace = %+v", pending)
	}
	if _, err := s.MarkWorkspaceBootstrapPending(ctx, other.project.ID, fixture.issue.ID, fixture.workspace.ID, path, "/repos/source", "main", fixture.workspace.WorkingBranch); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project pending error = %v", err)
	}
	if _, err := s.MarkWorkspaceBootstrapReady(ctx, fixture.project.ID, fixture.issue.ID, fixture.workspace.ID, path, "/repos/source", "main", "", fixture.workspace.WorkingBranch); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank base revision error = %v", err)
	}

	ready, err := s.MarkWorkspaceBootstrapReady(ctx, fixture.project.ID, fixture.issue.ID, fixture.workspace.ID, path, "/repos/source", "main", "0123456789abcdef", fixture.workspace.WorkingBranch)
	if err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	if ready.BootstrapStatus != "READY" || ready.BaseRevision == nil || *ready.BaseRevision != "0123456789abcdef" {
		t.Fatalf("ready workspace = %+v", ready)
	}
	if _, err := s.MarkWorkspaceBootstrapFailed(ctx, fixture.project.ID, fixture.issue.ID, fixture.workspace.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("READY workspace regressed: err=%v", err)
	}
	persisted, err := s.GetWorkspaceByIssue(ctx, fixture.project.ID, fixture.issue.ID)
	if err != nil || persisted.BootstrapStatus != "READY" || persisted.Path != path {
		t.Fatalf("persisted workspace = %+v err=%v", persisted, err)
	}
}

func TestWorkspaceBootstrapAdvisoryLockSerializesCallers(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	fixture := seedRunFixture(t, s, "workspace-lock")

	first, err := s.AcquireWorkspaceBootstrapLock(ctx, fixture.workspace.ID)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}

	type lockResult struct {
		lock store.WorkspaceBootstrapLock
		err  error
	}
	started := make(chan struct{})
	result := make(chan lockResult, 1)
	go func() {
		close(started)
		lock, err := s.AcquireWorkspaceBootstrapLock(context.Background(), fixture.workspace.ID)
		result <- lockResult{lock: lock, err: err}
	}()
	<-started

	select {
	case got := <-result:
		if got.lock != nil {
			_ = got.lock.Release()
		}
		t.Fatalf("second caller acquired lock before release: %v", got.err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := first.Release(); err != nil {
		t.Fatalf("release first lock: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("idempotent release: %v", err)
	}

	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("acquire second lock: %v", got.err)
		}
		if err := got.lock.Release(); err != nil {
			t.Fatalf("release second lock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second caller did not acquire after release")
	}
}

func TestWorkspaceBootstrapCancelledLockWaitDoesNotStrandSession(t *testing.T) {
	s := New(testPool(t))
	fixture := seedRunFixture(t, s, "workspace-lock-cancel")
	first, err := s.AcquireWorkspaceBootstrapLock(context.Background(), fixture.workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()

	waitCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if lock, err := s.AcquireWorkspaceBootstrapLock(waitCtx, fixture.workspace.ID); err == nil {
		_ = lock.Release()
		t.Fatal("cancelled lock wait unexpectedly succeeded")
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	third, err := s.AcquireWorkspaceBootstrapLock(context.Background(), fixture.workspace.ID)
	if err != nil {
		t.Fatalf("acquire after cancelled waiter: %v", err)
	}
	if err := third.Release(); err != nil {
		t.Fatalf("release after cancelled waiter: %v", err)
	}
}

func TestWorkspaceBootstrapLockWaitIsBoundedAndRetryable(t *testing.T) {
	s := New(testPool(t))
	fixture := seedRunFixture(t, s, "workspace-lock-timeout")
	first, err := s.AcquireWorkspaceBootstrapLock(context.Background(), fixture.workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()

	started := time.Now()
	lock, err := s.acquireWorkspaceBootstrapLock(context.Background(), fixture.workspace.ID, 50*time.Millisecond)
	if lock != nil {
		_ = lock.Release()
	}
	if !errors.Is(err, store.ErrWorkspaceBootstrapLockTimeout) {
		t.Fatalf("lock wait error = %v, want ErrWorkspaceBootstrapLockTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded lock wait took %v", elapsed)
	}
}
