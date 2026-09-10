package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
)

func TestStartOnRunnerConnectFailureMarksSessionFailed(t *testing.T) {
	service, storeFake, _ := runnerOwnedExecutionService(t)
	service.registry = &fakeExecutionManager{err: errors.New("runner offline")}

	_, err := service.StartOnRunner(t.Context(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}})
	if err == nil || !strings.Contains(err.Error(), "connect runner") {
		t.Fatalf("StartOnRunner error=%v, want connect runner failure", err)
	}
	if storeFake.session.Status != "FAILED" {
		t.Fatalf("execution session status=%q, want FAILED", storeFake.session.Status)
	}
}

func TestStartOnRunnerProtocolFailureMarksSessionFailed(t *testing.T) {
	service, storeFake, transport := runnerOwnedExecutionService(t)
	protocolErr := &runner.ProtocolError{Code: "start_rejected", Message: "runner rejected session"}
	service.registry = &fakeExecutionManager{client: &fakeExecutionClient{
		transport: transport,
		startErr:  protocolErr,
		done:      make(chan struct{}),
	}}

	_, err := service.StartOnRunner(t.Context(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}})
	var gotProtocolErr *runner.ProtocolError
	if !errors.As(err, &gotProtocolErr) || gotProtocolErr.Code != protocolErr.Code {
		t.Fatalf("StartOnRunner error=%v, want protocol error %q", err, protocolErr.Code)
	}
	if storeFake.session.Status != "FAILED" {
		t.Fatalf("execution session status=%q, want FAILED", storeFake.session.Status)
	}
}

func TestStartOnRunnerInterruptedStartRetainsSessionForReconciliation(t *testing.T) {
	service, storeFake, transport := runnerOwnedExecutionService(t)
	service.registry = &fakeExecutionManager{client: &fakeExecutionClient{
		transport: transport,
		startErr:  errors.New("transport interrupted"),
		done:      make(chan struct{}),
	}}

	_, err := service.StartOnRunner(t.Context(), "project-1", "run-1", "runner-1", ExecutionRequest{Command: []string{"true"}})
	if err == nil || !strings.Contains(err.Error(), "reconciliation is required") {
		t.Fatalf("StartOnRunner error=%v, want uncertain start", err)
	}
	if storeFake.session.Status != "STARTING" {
		t.Fatalf("execution session status=%q, want STARTING", storeFake.session.Status)
	}
	process, ok := service.liveProcess("project-1", storeFake.session.ID)
	if !ok {
		t.Fatal("uncertain runner start was not retained for reconciliation")
	}

	transport.result = runner.Result{ExitCode: 0}
	close(transport.resultCh)
	if _, waitErr := process.Wait(t.Context()); waitErr != nil {
		t.Fatalf("retained process completion error=%v", waitErr)
	}
	if process.Record().Status != "COMPLETED" {
		t.Fatalf("retained process status=%q, want COMPLETED", process.Record().Status)
	}
}
