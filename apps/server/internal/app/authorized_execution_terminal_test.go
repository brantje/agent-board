package app

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type abandoningExecutionTransport struct {
	*fakeExecutionTransport
}

func (t *abandoningExecutionTransport) AbandonStdout() error { return nil }
func (t *abandoningExecutionTransport) AbandonStderr() error { return nil }

func TestAuthorizedExecutionPreservesUnreadTerminalOutputBeforeRelease(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	transport.stdout = "stdout before plain-secret after"
	transport.stderr = "stderr before plain-secret after"
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
		t.Fatal("redaction registration was not released after terminal output was safely buffered")
	}

	stdout, err := io.ReadAll(process.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(process.Stderr())
	if err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"stdout": string(stdout), "stderr": string(stderr)} {
		if strings.Contains(output, "plain-secret") {
			t.Fatalf("%s leaked secret after Wait: %q", name, output)
		}
		if !strings.Contains(output, "before") || !strings.Contains(output, "after") {
			t.Fatalf("%s lost queued terminal output: %q", name, output)
		}
	}
}

func TestAuthorizedExecutionExplicitAbandonCompletesLifecycle(t *testing.T) {
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
	if process.ID() != "session-1" || process.Record().ID != "session-1" {
		t.Fatalf("unexpected process identity: id=%q record=%+v", process.ID(), process.Record())
	}
	if err := process.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	if err := process.AbandonStdout(); err != nil {
		t.Fatal(err)
	}
	if err := process.AbandonStderr(); err != nil {
		t.Fatal(err)
	}

	close(transport.resultCh)
	if _, err := process.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("redaction registration was not released after explicit output abandonment")
	}
}
