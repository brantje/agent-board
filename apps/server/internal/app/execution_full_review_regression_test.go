package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type failingKillTransport struct {
	*fakeExecutionTransport
	killErr error
}

func (t *failingKillTransport) Kill(context.Context) error {
	t.killed = true
	return t.killErr
}

type deadlineCheckingExecutionStore struct {
	*executionSessionStoreFake
	readyHadDeadline bool
}

func (s *deadlineCheckingExecutionStore) UpdateRuntimeInstanceRunnerStatus(ctx context.Context, projectID, instanceID, status string) (store.RuntimeInstance, error) {
	if status == "READY" {
		_, s.readyHadDeadline = ctx.Deadline()
		if !s.readyHadDeadline {
			return store.RuntimeInstance{}, errors.New("READY update missing deadline")
		}
	}
	return s.executionSessionStoreFake.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, instanceID, status)
}

func newExecutionServiceForTransport(t *testing.T, sessionStore ExecutionSessionStore, transport runner.ProcessSession) *ExecutionSessionService {
	t.Helper()
	client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
	service, err := NewExecutionSessionService(sessionStore, &fakeExecutionManager{client: client})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestFailedKillPreservesEarlierCancellationIntent(t *testing.T) {
	storeFake := &executionSessionStoreFake{
		run:      store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING", RunnerStatus: "READY"},
	}
	baseTransport := newFakeExecutionTransport("session-1")
	killErr := errors.New("kill failed")
	transport := &failingKillTransport{fakeExecutionTransport: baseTransport, killErr: killErr}
	service := newExecutionServiceForTransport(t, storeFake, transport)

	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"sleep", "30"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Terminate(context.Background()); err != nil {
		t.Fatalf("Terminate() error=%v", err)
	}
	if err := process.Kill(context.Background()); !errors.Is(err, killErr) {
		t.Fatalf("Kill() error=%v", err)
	}

	baseTransport.result = runner.Result{ExitCode: 143, Signaled: true}
	close(baseTransport.resultCh)
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := process.Wait(waitCtx); err != nil {
		t.Fatalf("Wait() error=%v", err)
	}
	if process.Record().Status != "CANCELLED" {
		t.Fatalf("record=%+v", process.Record())
	}
}

func TestTerminalRunnerReadyWriteUsesRecoveryDeadline(t *testing.T) {
	baseStore := &executionSessionStoreFake{
		run:      store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING", RunnerStatus: "READY"},
	}
	storeFake := &deadlineCheckingExecutionStore{executionSessionStoreFake: baseStore}
	transport := newFakeExecutionTransport("session-1")
	service := newExecutionServiceForTransport(t, storeFake, transport)

	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	transport.result = runner.Result{ExitCode: 0}
	close(transport.resultCh)
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := process.Wait(waitCtx); err != nil {
		t.Fatalf("Wait() error=%v", err)
	}
	if !storeFake.readyHadDeadline {
		t.Fatal("terminal READY update did not use a bounded recovery context")
	}
}
