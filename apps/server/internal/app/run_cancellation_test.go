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

type delegationCancelStore struct {
	store.ControlPlaneStore
	runs        map[string]store.Run
	delegations []store.Delegation
	cancelled   []string
}

func (s *delegationCancelStore) GetProject(context.Context, string) (store.Project, error) {
	return store.Project{ID: "project-1"}, nil
}

func (s *delegationCancelStore) GetRun(_ context.Context, _, id string) (store.Run, error) {
	run, ok := s.runs[id]
	if !ok {
		return store.Run{}, store.ErrNotFound
	}
	return run, nil
}

func (s *delegationCancelStore) CancelInactiveRun(_ context.Context, _, id string) (store.RunCancellationResult, error) {
	run, ok := s.runs[id]
	if !ok {
		return store.RunCancellationResult{}, store.ErrNotFound
	}
	if run.Status != "QUEUED" && run.Status != "PAUSED" {
		return store.RunCancellationResult{}, store.ErrConflict
	}
	run.Status = "CANCELLED"
	s.runs[id] = run
	s.cancelled = append(s.cancelled, id)
	return store.RunCancellationResult{Run: run, Event: store.Event{ID: "cancel-" + id, Type: "run.cancelled", ProjectID: run.ProjectID}}, nil
}

func (s *delegationCancelStore) RequestDelegation(context.Context, store.RequestDelegationCommand) (store.RequestDelegationResult, error) {
	return store.RequestDelegationResult{}, store.ErrConflict
}
func (s *delegationCancelStore) GetDelegationByRun(context.Context, string, string) (store.Delegation, error) {
	return store.Delegation{}, store.ErrNotFound
}
func (s *delegationCancelStore) ListDelegationsByParentRun(_ context.Context, _, parent string) ([]store.Delegation, error) {
	var out []store.Delegation
	for _, d := range s.delegations {
		if d.ParentRunID == parent {
			out = append(out, d)
		}
	}
	return out, nil
}

func TestCancelRunDurablyCancelsInactiveParentAndQueuedDelegate(t *testing.T) {
	base := &delegationCancelStore{
		runs: map[string]store.Run{
			"parent": {ID: "parent", ProjectID: "project-1", Status: "PAUSED"},
			"child":  {ID: "child", ProjectID: "project-1", Status: "QUEUED"},
		},
		delegations: []store.Delegation{{ID: "delegation-1", ProjectID: "project-1", ParentRunID: "parent", DelegatedRunID: "child"}},
	}
	services := &Services{ControlPlane: New(base), Scheduler: &scheduler.Coordinator{}}
	if err := services.CancelRun(t.Context(), "project-1", "parent"); err != nil {
		t.Fatal(err)
	}
	if got := base.runs["parent"].Status; got != "CANCELLED" {
		t.Fatalf("parent status=%s want CANCELLED", got)
	}
	if got := base.runs["child"].Status; got != "CANCELLED" {
		t.Fatalf("child status=%s want CANCELLED", got)
	}
	if len(base.cancelled) != 2 || base.cancelled[0] != "parent" || base.cancelled[1] != "child" {
		t.Fatalf("cancellation order=%v", base.cancelled)
	}
}
