package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type fakeExecutionPreparer struct {
	prepared executioncontext.Prepared
	err      error
	calls    int
	request  executioncontext.SecretRequest
}

func (f *fakeExecutionPreparer) Prepare(_ context.Context, _, _ string, request executioncontext.SecretRequest) (executioncontext.Prepared, error) {
	f.calls++
	f.request = request
	return f.prepared, f.err
}

func TestAuthorizedExecutionResolvesBeforeInjectingSecretsAndRedactsRunnerOutput(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	transport.stdout = "before plain-secret after"
	transport.stderr = "runner-error plain-secret"
	client := &requestCapturingClient{fakeExecutionClient: &fakeExecutionClient{transport: transport, done: make(chan struct{})}}
	storeFake := &executionSessionStoreFake{
		run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", RuntimeID: "runtime-config-1", Status: "RUNNING", RunnerStatus: "READY"},
	}
	lowLevel, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client})
	if err != nil {
		t.Fatal(err)
	}
	preparer := &fakeExecutionPreparer{prepared: executioncontext.Prepared{
		RuntimeID:       "runtime-config-1",
		Secrets:         map[string]string{"TOKEN": "plain-secret"},
		RedactionValues: []string{"plain-secret"},
	}}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, preparer)
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{
		Command:           []string{"true"},
		Env:               map[string]string{"SAFE": "value"},
		RuntimeSecretRefs: map[string]string{"TOKEN": "runtime-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if preparer.calls != 1 || preparer.request.RuntimeSecretRefs["TOKEN"] != "runtime-token" {
		t.Fatalf("preparer calls=%d request=%+v", preparer.calls, preparer.request)
	}
	if client.request.Secrets["TOKEN"] != "plain-secret" || client.request.Env["SAFE"] != "value" {
		t.Fatalf("runner request = %+v", client.request)
	}
	stdout, err := io.ReadAll(process.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(process.Stderr())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stdout), "plain-secret") || strings.Contains(string(stderr), "plain-secret") {
		t.Fatalf("runner output leaked secret: stdout=%q stderr=%q", stdout, stderr)
	}
	close(transport.resultCh)
	_, _ = process.Wait(context.Background())
}

func TestAuthorizedExecutionRedactsStartErrors(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	client := &fakeExecutionClient{
		transport: transport,
		startErr:  errors.New("runner rejected plain-secret"),
		done:      make(chan struct{}),
	}
	storeFake := &executionSessionStoreFake{
		run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", RuntimeID: "runtime-config-1", Status: "RUNNING", RunnerStatus: "READY"},
	}
	lowLevel, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client})
	if err != nil {
		t.Fatal(err)
	}
	preparer := &fakeExecutionPreparer{prepared: executioncontext.Prepared{
		RuntimeID:       "runtime-config-1",
		Secrets:         map[string]string{"TOKEN": "plain-secret"},
		RedactionValues: []string{"plain-secret"},
	}}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, preparer)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Start(context.Background(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{Command: []string{"true"}})
	if err == nil {
		t.Fatal("expected runner start failure")
	}
	if strings.Contains(err.Error(), "plain-secret") {
		t.Fatalf("start error leaked secret: %v", err)
	}
}

func TestAuthorizedExecutionRejectsRuntimeMismatchBeforeSessionCreation(t *testing.T) {
	storeFake := &executionSessionStoreFake{
		run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", RuntimeID: "runtime-a", Status: "RUNNING"},
	}
	lowLevel, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: &fakeExecutionClient{transport: newFakeExecutionTransport("session-1"), done: make(chan struct{})}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{prepared: executioncontext.Prepared{RuntimeID: "runtime-b"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{Command: []string{"true"}}); err == nil {
		t.Fatal("expected Runtime mismatch")
	}
	if storeFake.session.ID != "" {
		t.Fatalf("execution session persisted before preflight completed: %+v", storeFake.session)
	}
}

type requestCapturingClient struct {
	*fakeExecutionClient
	request runner.Request
}

func (c *requestCapturingClient) Start(ctx context.Context, sessionID string, request runner.Request) (runner.ProcessSession, error) {
	c.request = request
	return c.fakeExecutionClient.Start(ctx, sessionID, request)
}
