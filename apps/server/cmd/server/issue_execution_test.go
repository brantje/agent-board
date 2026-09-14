package main

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type startupExecutionStore struct {
	store.ControlPlaneStore
	calls  int
	err    error
	filter store.IssueExecutionFilter
	cancel context.CancelFunc
}

func (s *startupExecutionStore) StartIssueRun(context.Context, string, string) (store.Run, store.Event, error) {
	panic("startup must reconcile")
}
func (s *startupExecutionStore) ReconcileIssueExecution(_ context.Context, f store.IssueExecutionFilter) ([]store.Event, error) {
	s.calls++
	s.filter = f
	s.cancel()
	return nil, s.err
}
func TestStartSchedulerRecoversIssueExecutionBeforeClaims(t *testing.T) {
	for _, failure := range []error{nil, errors.New("recovery unavailable")} {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		f := &startupExecutionStore{err: failure, cancel: cancel}
		h := &applicationHandler{services: &app.Services{ControlPlane: app.New(f), Scheduler: &scheduler.Coordinator{}}}
		done, err := startScheduler(ctx, h)
		if !errors.Is(err, failure) || f.calls != 1 || f.filter != (store.IssueExecutionFilter{}) {
			t.Fatalf("startup recovery: %+v %v", f, err)
		}
		if failure != nil {
			if done != nil {
				t.Fatal("scheduler started after recovery failure")
			}
		} else {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
	}
}
