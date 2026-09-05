package app

import (
	"context"
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
)

func TestRuntimeInstanceServiceDestroyContinuesAfterStopFailure(t *testing.T) {
	t.Run("successful destroy persists destroyed state", func(t *testing.T) {
		service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
		ctx := context.Background()
		instance, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		instance, err = service.Start(ctx, workspace.ProjectID, instance.ID)
		if err != nil {
			t.Fatal(err)
		}

		stopErr := errors.New("stop failed")
		implementation.stopErr = stopErr
		instance, err = service.Destroy(ctx, workspace.ProjectID, instance.ID)
		if !errors.Is(err, stopErr) {
			t.Fatalf("Destroy() error=%v", err)
		}
		if instance.Status != string(runtimepkg.StateDestroyed) || runtimeStore.instance.Status != string(runtimepkg.StateDestroyed) {
			t.Fatalf("Destroy() instance=%+v persisted=%+v", instance, runtimeStore.instance)
		}
		if implementation.stopCalls != 1 || implementation.destroyCalls != 1 {
			t.Fatalf("Destroy() stopCalls=%d destroyCalls=%d", implementation.stopCalls, implementation.destroyCalls)
		}
	})

	t.Run("stop and destroy failures are joined", func(t *testing.T) {
		service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
		ctx := context.Background()
		instance, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		instance, err = service.Start(ctx, workspace.ProjectID, instance.ID)
		if err != nil {
			t.Fatal(err)
		}

		stopErr := errors.New("stop failed")
		destroyErr := errors.New("destroy failed")
		implementation.stopErr = stopErr
		implementation.destroyErr = destroyErr
		instance, err = service.Destroy(ctx, workspace.ProjectID, instance.ID)
		if !errors.Is(err, stopErr) || !errors.Is(err, destroyErr) {
			t.Fatalf("Destroy() error=%v", err)
		}
		if instance.Status != string(runtimepkg.StateFailed) || runtimeStore.instance.Status != string(runtimepkg.StateFailed) {
			t.Fatalf("Destroy() instance=%+v persisted=%+v", instance, runtimeStore.instance)
		}
		if implementation.stopCalls != 1 || implementation.destroyCalls != 1 {
			t.Fatalf("Destroy() stopCalls=%d destroyCalls=%d", implementation.stopCalls, implementation.destroyCalls)
		}
	})
}
