package app

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
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

func TestAuthorizedExecutionConstructorAndPreparationErrors(t *testing.T) {
	preparer := &fakeExecutionPreparer{}
	if _, err := NewAuthorizedExecutionSessionService(nil, preparer); err == nil {
		t.Fatal("expected nil session service rejection")
	}
	lowLevel, _, _, _ := runnerOwnedExecutionService(t)
	if _, err := NewAuthorizedExecutionSessionService(lowLevel, nil); err == nil {
		t.Fatal("expected nil preparer rejection")
	}

	cause := errors.New("configuration lookup failed")
	preparer.err = &executioncontext.Error{Code: "execution_context_unavailable", Message: "Execution context is unavailable", Cause: cause}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, preparer)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", AuthorizedExecutionRequest{Command: []string{"true"}})
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_context_unavailable" || !errors.Is(err, cause) {
		t.Fatalf("translated preparation error=%v api=%+v", err, apiErr)
	}
}

func TestAuthorizedExecutionResolvesProviderSecretAndRedactsRunnerOutput(t *testing.T) {
	lowLevel, _, transport, client := runnerOwnedExecutionService(t)
	transport.stdout = "before plain-secret after"
	transport.stderr = "runner-error plain-secret"
	released := make(chan struct{})
	preparer := &fakeExecutionPreparer{prepared: executioncontext.Prepared{
		Secrets:          map[string]string{"PROVIDER_TOKEN": "plain-secret"},
		RedactionValues:  []string{"plain-secret"},
		ReleaseRedaction: func() { close(released) },
	}}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, preparer)
	if err != nil {
		t.Fatal(err)
	}
	process, err := service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", AuthorizedExecutionRequest{
		Command:               []string{"true"},
		Env:                   map[string]string{"SAFE": "value"},
		ProviderCredentialEnv: "PROVIDER_TOKEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preparer.calls != 1 || preparer.request.ProviderCredentialEnv != "PROVIDER_TOKEN" {
		t.Fatalf("preparer calls=%d request=%+v", preparer.calls, preparer.request)
	}
	if client.request.Secrets["PROVIDER_TOKEN"] != "plain-secret" || client.request.Env["SAFE"] != "value" {
		t.Fatalf("runner request=%+v", client.request)
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
	assertRunRedactionRetainedAndRelease(t, service, "run-1", released)
}

func TestAuthorizedExecutionProcessDoesNotExposeRawProcess(t *testing.T) {
	processType := reflect.TypeOf(AuthorizedExecutionProcess{})
	rawType := reflect.TypeOf((*ExecutionProcess)(nil))
	for index := 0; index < processType.NumField(); index++ {
		field := processType.Field(index)
		if field.Type == rawType && field.PkgPath == "" {
			t.Fatalf("raw ExecutionProcess is exported through field %q", field.Name)
		}
	}
}

func TestAuthorizedExecutionRedactsStartErrorsAndReleasesRegistration(t *testing.T) {
	lowLevel, _, _, client := runnerOwnedExecutionService(t)
	client.startErr = errors.New("runner rejected plain-secret")
	releases := 0
	preparer := &fakeExecutionPreparer{prepared: executioncontext.Prepared{
		Secrets:          map[string]string{"PROVIDER_TOKEN": "plain-secret"},
		RedactionValues:  []string{"plain-secret"},
		ReleaseRedaction: func() { releases++ },
	}}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, preparer)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", AuthorizedExecutionRequest{Command: []string{"true"}})
	if err == nil {
		t.Fatal("expected runner start failure")
	}
	if strings.Contains(err.Error(), "plain-secret") {
		t.Fatalf("start error leaked secret: %v", err)
	}
	if releases != 1 {
		t.Fatalf("redaction releases=%d, want 1", releases)
	}
}

func TestAuthorizedCreateAndStartPreparedRunnerSession(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
	registry := &fakeExecutionManager{client: client}
	storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"}}
	lowLevel, err := NewExecutionSessionService(storeFake, registry)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{prepared: executioncontext.Prepared{
		Secrets: map[string]string{"AGENT_BOARD_PROVIDER_API_KEY": "provider-secret"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateRunnerSession(context.Background(), "project-1", "run-1", "runner-1")
	if err != nil || session.RunnerID != "runner-1" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	process, err := service.StartPreparedOnRunner(context.Background(), "project-1", session.ID, AuthorizedExecutionRequest{Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	if process.Record().RunnerID != "runner-1" {
		t.Fatalf("record=%+v", process.Record())
	}
	close(transport.resultCh)
	if _, err := process.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func assertRunRedactionRetainedAndRelease(t *testing.T, service *AuthorizedExecutionSessionService, runID string, released <-chan struct{}) {
	t.Helper()
	service.redactionMu.Lock()
	_, retained := service.runRedactions[runID]
	service.redactionMu.Unlock()
	if !retained {
		t.Fatalf("Run %q redaction lease was not retained", runID)
	}
	select {
	case <-released:
		t.Fatal("Run redaction lease released before Processor completion")
	default:
	}
	service.ReleaseRunRedaction(runID)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("Run redaction lease was not released")
	}
	service.ReleaseRunRedaction(runID)
}

func TestAuthorizedExecutionAttachReusesLiveRunnerSession(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
	registry := &fakeExecutionManager{client: client, reconcile: transport, active: true}
	storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"}}
	lowLevel, err := NewExecutionSessionService(storeFake, registry)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartOnRunner(t.Context(), "project-1", "run-1", "runner-1", AuthorizedExecutionRequest{Command: []string{"sleep", "10"}})
	if err != nil {
		t.Fatal(err)
	}
	attached, err := service.Attach(t.Context(), "project-1", started.ID())
	if err != nil {
		t.Fatal(err)
	}
	if attached.ID() != started.ID() || storeFake.session.ID != started.ID() {
		t.Fatalf("attached=%q started=%q session=%+v", attached.ID(), started.ID(), storeFake.session)
	}
	close(transport.resultCh)
}

func TestAuthorizedExecutionAttachPreparesAuthorizedSecretRedaction(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
	registry := &fakeExecutionManager{client: client, reconcile: transport, active: true}
	storeFake := &executionSessionStoreFake{run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"}}
	lowLevel, err := NewExecutionSessionService(storeFake, registry)
	if err != nil {
		t.Fatal(err)
	}
	preparer := &fakeExecutionPreparer{prepared: executioncontext.Prepared{}}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, preparer)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartOnRunner(t.Context(), "project-1", "run-1", "runner-1", AuthorizedExecutionRequest{
		Command:               []string{"sleep", "10"},
		ProviderCredentialEnv: "PROVIDER_TOKEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preparer.request.RedactAuthorizedSecrets {
		t.Fatal("Start must not switch to attach redaction")
	}
	_, err = service.Attach(t.Context(), "project-1", started.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !preparer.request.RedactAuthorizedSecrets {
		t.Fatalf("Attach SecretRequest=%+v", preparer.request)
	}
	close(transport.resultCh)
}

func TestAuthorizedExecutionAttachFailureBoundaries(t *testing.T) {
	t.Run("unavailable service", func(t *testing.T) {
		var service *AuthorizedExecutionSessionService
		_, err := service.Attach(t.Context(), "project-1", "session-1")
		apiErr, ok := AsError(err)
		if !ok || apiErr.Code != "execution_session_unavailable" {
			t.Fatalf("err=%v api=%+v", err, apiErr)
		}
	})

	t.Run("session not running", func(t *testing.T) {
		lowLevel, storeFake, _, _ := runnerOwnedExecutionService(t)
		storeFake.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: "COMPLETED"}
		service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Attach(t.Context(), "project-1", "session-1")
		apiErr, ok := AsError(err)
		if !ok || apiErr.Code != "execution_session_not_running" {
			t.Fatalf("err=%v api=%+v", err, apiErr)
		}
	})
}
