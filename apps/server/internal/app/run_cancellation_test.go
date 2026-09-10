package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type cancelRunStore struct {
	store.ControlPlaneStore
	run store.Run
	err error
}

func (s *cancelRunStore) GetProject(context.Context, string) (store.Project, error) {
	return store.Project{ID: "project-1"}, nil
}

func (s *cancelRunStore) GetRun(context.Context, string, string) (store.Run, error) {
	return s.run, s.err
}

func TestCancelRunRejectsUnavailableInvalidTerminalAndUnownedRuns(t *testing.T) {
	if err := (&Services{}).CancelRun(t.Context(), "project-1", "run-1"); err == nil {
		t.Fatal("cancellation without services unexpectedly succeeded")
	}

	activeStore := &cancelRunStore{run: store.Run{ID: "run-1", ProjectID: "project-1", Status: "RUNNING"}}
	services := &Services{ControlPlane: New(activeStore), Scheduler: &scheduler.Coordinator{}}
	if err := services.CancelRun(t.Context(), "", "run-1"); err == nil {
		t.Fatal("cancellation with blank project id unexpectedly succeeded")
	}
	if err := services.CancelRun(t.Context(), "project-1", "run-1"); err == nil {
		t.Fatal("unowned run cancellation unexpectedly succeeded")
	}

	activeStore.run.Status = "COMPLETED"
	if err := services.CancelRun(t.Context(), "project-1", "run-1"); err == nil {
		t.Fatal("terminal run cancellation unexpectedly succeeded")
	}

	activeStore.run.Status = "RUNNING"
	activeStore.err = store.ErrNotFound
	if err := services.CancelRun(t.Context(), "project-1", "run-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetRun error=%v, want ErrNotFound", err)
	}
}
