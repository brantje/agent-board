package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestReconcileRetainsActiveProcessWhenBusyPersistenceFails(t *testing.T) {
	statusErr := errors.New("persist BUSY failed")
	base := &executionSessionStoreFake{
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING", RunnerStatus: "READY"},
		session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "RUNNING"},
	}
	storeWithFailure := &reconcileStatusFailureStore{executionSessionStoreFake: base, err: statusErr}
	transport := &reconcileDrainTransport{
		fakeExecutionTransport: newFakeExecutionTransport("session-1"),
		stdout:                 newReadSignalReader(),
		stderr:                 newReadSignalReader(),
	}
	service, err := NewExecutionSessionService(storeWithFailure, &reconcileExecutionManager{transport: transport, active: true})
	if err != nil {
		t.Fatal(err)
	}

	process, err := service.Reconcile(context.Background(), "project-1", "session-1")
	if err == nil || process != nil {
		t.Fatalf("process=%v err=%v", process, err)
	}
	retained, ok := service.liveProcess("project-1", "session-1")
	if !ok || retained == nil {
		t.Fatal("active reconciled process was not retained after BUSY persistence failure")
	}
	for name, drained := range map[string]<-chan struct{}{"stdout": transport.stdout.read, "stderr": transport.stderr.read} {
		select {
		case <-drained:
		case <-time.After(time.Second):
			t.Fatalf("%s was not drained by retained process", name)
		}
	}

	transport.result = runner.Result{ExitCode: 17}
	close(transport.resultCh)
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, waitErr := retained.Wait(waitCtx)
	if !errors.Is(waitErr, statusErr) {
		t.Fatalf("Wait() error=%v, want runner status persistence error", waitErr)
	}
	if base.session.Status != "COMPLETED" || base.session.ExitCode == nil || *base.session.ExitCode != 17 {
		t.Fatalf("terminal result was not persisted: session=%+v", base.session)
	}
}
