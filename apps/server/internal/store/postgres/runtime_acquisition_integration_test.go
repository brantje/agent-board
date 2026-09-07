package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestRuntimeAcquisitionLockSerializesWorkspaceRuntimePair(t *testing.T) {
	s := New(testPool(t))
	fixture := seedRunFixture(t, s, "runtime-acquisition-lock")

	first, err := s.AcquireRuntimeAcquisitionLock(t.Context(), fixture.workspace.ID, fixture.runtime.ID)
	if err != nil {
		t.Fatalf("acquire first Runtime lock: %v", err)
	}

	blockedCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if second, err := s.AcquireRuntimeAcquisitionLock(blockedCtx, fixture.workspace.ID, fixture.runtime.ID); !errors.Is(err, context.DeadlineExceeded) {
		if second != nil {
			_ = second.Release()
		}
		t.Fatalf("contending Runtime lock error=%v, want context deadline", err)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("release first Runtime lock: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("second release must be idempotent: %v", err)
	}

	second, err := s.AcquireRuntimeAcquisitionLock(t.Context(), fixture.workspace.ID, fixture.runtime.ID)
	if err != nil {
		t.Fatalf("acquire Runtime lock after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release second Runtime lock: %v", err)
	}
}

func TestRuntimeAcquisitionLockValidatesBindings(t *testing.T) {
	s := New(testPool(t))
	if _, err := s.AcquireRuntimeAcquisitionLock(t.Context(), "", "runtime-1"); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank workspace error=%v", err)
	}
	if _, err := s.AcquireRuntimeAcquisitionLock(t.Context(), "workspace-1", ""); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank runtime error=%v", err)
	}
}
