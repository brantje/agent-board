package app

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type abandoningExecutionTransport struct {
	*fakeExecutionTransport
}

func (t *abandoningExecutionTransport) AbandonStdout() error { return nil }
func (t *abandoningExecutionTransport) AbandonStderr() error { return nil }

func TestAuthorizedExecutionPreservesUnreadTerminalOutputBeforeRelease(t *testing.T) {
	lowLevel, _, transport, _ := runnerOwnedExecutionService(t)
	transport.stdout = "stdout before plain-secret after"
	transport.stderr = "stderr before plain-secret after"
	released := make(chan struct{})
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{prepared: executioncontext.Prepared{
		RedactionValues:  []string{"plain-secret"},
		ReleaseRedaction: func() { close(released) },
	}})
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", AuthorizedExecutionRequest{Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}

	close(transport.resultCh)
	if _, err := process.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRunRedactionRetainedAndRelease(t, service, "run-1", released)

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
	registry := &fakeExecutionManager{client: client}
	storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"}}
	lowLevel, err := NewExecutionSessionService(storeFake, registry)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{prepared: executioncontext.Prepared{
		RedactionValues:  []string{"plain-secret"},
		ReleaseRedaction: func() { close(released) },
	}})
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", AuthorizedExecutionRequest{Command: []string{"true"}})
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
	assertRunRedactionRetainedAndRelease(t, service, "run-1", released)
}
