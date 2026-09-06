package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type postStartPersistenceStore struct {
	*branchExecutionStore
	runningErr error
	busyErr    error
}

func (s *postStartPersistenceStore) TransitionExecutionSession(ctx context.Context, transition store.ExecutionSessionTransition) (store.ExecutionSession, error) {
	if transition.Status == "RUNNING" && s.runningErr != nil {
		return store.ExecutionSession{}, s.runningErr
	}
	return s.branchExecutionStore.TransitionExecutionSession(ctx, transition)
}

func (s *postStartPersistenceStore) UpdateRuntimeInstanceRunnerStatus(ctx context.Context, projectID, instanceID, status string) (store.RuntimeInstance, error) {
	if status == "BUSY" && s.busyErr != nil {
		return store.RuntimeInstance{}, s.busyErr
	}
	return s.branchExecutionStore.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, instanceID, status)
}

func TestExecutionServiceRetainsProcessAfterPostStartPersistenceFailure(t *testing.T) {
	tests := []struct {
		name             string
		wantStatus       string
		wantRunnerStatus string
		configure        func(*postStartPersistenceStore) error
		clear            func(*postStartPersistenceStore)
	}{
		{
			name:             "running transition",
			wantStatus:       "STARTING",
			wantRunnerStatus: "BUSY",
			configure: func(s *postStartPersistenceStore) error {
				err := errors.New("running transition failed")
				s.runningErr = err
				return err
			},
			clear: func(s *postStartPersistenceStore) { s.runningErr = nil },
		},
		{
			name:             "runner busy status",
			wantStatus:       "RUNNING",
			wantRunnerStatus: "READY",
			configure: func(s *postStartPersistenceStore) error {
				err := errors.New("runner busy status failed")
				s.busyErr = err
				return err
			},
			clear: func(s *postStartPersistenceStore) { s.busyErr = nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeFake := &postStartPersistenceStore{branchExecutionStore: newBranchExecutionStore()}
			wantErr := tt.configure(storeFake)
			transport := newFakeExecutionTransport("session-1")
			service := newBranchExecutionService(t, storeFake, transport, nil)

			process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}})
			if process != nil || err == nil || !errors.Is(err, wantErr) {
				t.Fatalf("Start() process=%v error=%v", process, err)
			}
			live, ok := service.liveProcess("project-1", "session-1")
			if !ok || live == nil || storeFake.session.Status != tt.wantStatus || storeFake.instance.RunnerStatus != tt.wantRunnerStatus {
				t.Fatalf("live=%v session=%+v instance=%+v", live, storeFake.session, storeFake.instance)
			}

			tt.clear(storeFake)
			transport.result = runner.Result{ExitCode: 0}
			close(transport.resultCh)
			waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, waitErr := live.Wait(waitCtx)
			cancel()
			if waitErr != nil {
				t.Fatalf("retained process Wait() error=%v", waitErr)
			}
			if live.Record().Status != "COMPLETED" || storeFake.session.Status != "COMPLETED" {
				t.Fatalf("live record=%+v durable session=%+v", live.Record(), storeFake.session)
			}
		})
	}
}
