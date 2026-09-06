package app

import (
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestAuthorizedExecutionProcessDelegatesLifecycleSignals(t *testing.T) {
	lowLevel, _, transport := executionServiceFixture(t)
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{prepared: executioncontext.Prepared{}})
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.Start(t.Context(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{Command: []string{"sleep", "10"}})
	if err != nil {
		t.Fatal(err)
	}

	if err := process.Terminate(t.Context()); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if err := process.Kill(t.Context()); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !transport.terminated || !transport.killed {
		t.Fatalf("transport signals terminate=%v kill=%v", transport.terminated, transport.killed)
	}
	close(transport.resultCh)
}

func TestAuthorizedExecutionServiceDelegatesMaintenanceOperations(t *testing.T) {
	lowLevel, _, _ := executionServiceFixture(t)
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{prepared: executioncontext.Prepared{}})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.ReconcileAll(t.Context()); err == nil {
		t.Fatal("expected unsupported reconciliation store error")
	}
	if err := service.ReconcileAllWithReporter(t.Context(), func(error) {}); err == nil {
		t.Fatal("expected unsupported reconciliation store error")
	}
	if err := service.Cancel(t.Context(), "", "", time.Millisecond); err == nil {
		t.Fatal("expected invalid cancellation identifiers to be rejected")
	} else {
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != "invalid_argument" {
			t.Fatalf("cancel error=%v", err)
		}
	}
}
