package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type abandoningExecutionTransport struct {
	*fakeExecutionTransport
	mu              sync.Mutex
	stdoutAbandoned bool
	stderrAbandoned bool
}

func (t *abandoningExecutionTransport) AbandonStdout() error {
	t.mu.Lock()
	t.stdoutAbandoned = true
	t.mu.Unlock()
	return nil
}

func (t *abandoningExecutionTransport) AbandonStderr() error {
	t.mu.Lock()
	t.stderrAbandoned = true
	t.mu.Unlock()
	return nil
}

func (t *abandoningExecutionTransport) abandoned() (bool, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stdoutAbandoned, t.stderrAbandoned
}

func TestAuthorizedExecutionSettlesUnreadTerminalOutputBeforeRelease(t *testing.T) {
	transport := &abandoningExecutionTransport{fakeExecutionTransport: newFakeExecutionTransport("session-1")}
	client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
	storeFake := &executionSessionStoreFake{
		run:      store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", RuntimeID: "runtime-config-1", Status: "RUNNING", RunnerStatus: "READY"},
	}
	lowLevel, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client})
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{prepared: executioncontext.Prepared{
		RuntimeID:        "runtime-config-1",
		RedactionValues:  []string{"plain-secret"},
		ReleaseRedaction: func() { close(released) },
	}})
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}

	close(transport.resultCh)
	if _, err := process.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("redaction registration was not released after terminal unread output was settled")
	}
	stdoutAbandoned, stderrAbandoned := transport.abandoned()
	if !stdoutAbandoned || !stderrAbandoned {
		t.Fatalf("terminal output settlement stdout=%v stderr=%v", stdoutAbandoned, stderrAbandoned)
	}
}
