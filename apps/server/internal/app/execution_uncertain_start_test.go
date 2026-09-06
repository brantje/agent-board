package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
)

func TestExecutionServiceRetainsAndReconcilesUncertainStart(t *testing.T) {
	storeFake := newBranchExecutionStore()
	transport := newFakeExecutionTransport("session-1")
	service := newBranchExecutionService(t, storeFake, transport, context.DeadlineExceeded)
	if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
		t.Fatal("expected uncertain start error")
	}
	live, ok := service.liveProcess("project-1", "session-1")
	if !ok || live == nil || storeFake.session.Status != "STARTING" || storeFake.instance.RunnerStatus != "BUSY" {
		t.Fatalf("live=%v session=%+v instance=%+v", live, storeFake.session, storeFake.instance)
	}
	reconciled, err := service.Reconcile(context.Background(), "project-1", "session-1")
	if err != nil || reconciled != live {
		t.Fatalf("reconciled=%v live=%v err=%v", reconciled, live, err)
	}
	transport.result = runner.Result{ExitCode: 0}
	close(transport.resultCh)
	if _, err := live.Wait(context.Background()); err != nil || live.Record().Status != "COMPLETED" {
		t.Fatalf("record=%+v err=%v", live.Record(), err)
	}
	if _, ok := service.liveProcess("project-1", "session-1"); ok {
		t.Fatal("terminal process remained tracked")
	}
}

func TestExecutionServiceRetainsUncertainStartWhenBusyPersistenceFails(t *testing.T) {
	storeFake := newBranchExecutionStore()
	statusErr := errors.New("runner status write failed")
	storeFake.updateRunnerErr = statusErr
	transport := newFakeExecutionTransport("session-1")
	service := newBranchExecutionService(t, storeFake, transport, context.DeadlineExceeded)
	_, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}})
	if err == nil || !errors.Is(err, statusErr) {
		t.Fatalf("Start() error=%v", err)
	}
	live, ok := service.liveProcess("project-1", "session-1")
	if !ok || live == nil || storeFake.session.Status != "STARTING" {
		t.Fatalf("live=%v session=%+v", live, storeFake.session)
	}
	storeFake.updateRunnerErr = nil
	transport.result = runner.Result{ExitCode: 0}
	close(transport.resultCh)
	if _, err := live.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error=%v", err)
	}
}
