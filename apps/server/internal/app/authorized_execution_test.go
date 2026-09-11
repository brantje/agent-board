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

func TestAuthorizedExecutionConstructorAndPreparationErrors(t *testing.T) {
	preparer := &fakeExecutionPreparer{}
	if _, err := NewAuthorizedExecutionSessionService(nil, preparer); err == nil {
		t.Fatal("expected nil session service rejection")
	}
	lowLevel, _, _ := executionServiceFixture(t)
	if _, err := NewAuthorizedExecutionSessionService(lowLevel, nil); err == nil {
		t.Fatal("expected nil preparer rejection")
	}

	cause := errors.New("configuration lookup failed")
	preparer.err = &executioncontext.Error{Code: "execution_context_unavailable", Message: "Execution context is unavailable", Cause: cause}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, preparer)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Start(context.Background(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{Command: []string{"true"}})
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_context_unavailable" || !errors.Is(err, cause) {
		t.Fatalf("translated preparation error=%v api=%+v", err, apiErr)
	}
}

func TestAuthorizedExecutionResolvesBeforeInjectingSecretsAndRedactsRunnerOutput(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	transport.stdout = "before plain-secret after"
	transport.stderr = "runner-error plain-secret"
	client := &requestCapturingClient{fakeExecutionClient: &fakeExecutionClient{transport: transport, done: make(chan struct{})}}
	storeFake := &executionSessionStoreFake{
		run:      store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING", RunnerStatus: "READY"},
	}
	lowLevel, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client}, nil)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	preparer := &fakeExecutionPreparer{prepared: executioncontext.Prepared{
				Secrets:          map[string]string{"TOKEN": "plain-secret"},
		RedactionValues:  []string{"plain-secret"},
		ReleaseRedaction: func() { close(released) },
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
	transport := newFakeExecutionTransport("session-1")
	client := &fakeExecutionClient{
		transport: transport,
		startErr:  errors.New("runner rejected plain-secret"),
		done:      make(chan struct{}),
	}
	storeFake := &executionSessionStoreFake{
		run:      store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", Status: "RUNNING", RunnerStatus: "READY"},
	}
	lowLevel, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client}, nil)
	if err != nil {
		t.Fatal(err)
	}
	releases := 0
	preparer := &fakeExecutionPreparer{prepared: executioncontext.Prepared{
				Secrets:          map[string]string{"TOKEN": "plain-secret"},
		RedactionValues:  []string{"plain-secret"},
		ReleaseRedaction: func() { releases++ },
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
	if releases != 1 {
		t.Fatalf("redaction releases=%d, want 1", releases)
	}
}

func TestAuthorizedExecutionStartOnRunnerRejectsRuntimeSecretRefs(t *testing.T) {
	lowLevel, err := NewExecutionSessionService(&executionSessionStoreFake{}, &fakeExecutionManager{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.StartOnRunner(context.Background(), "project-1", "run-1", "runner-1", AuthorizedExecutionRequest{
		Command:           []string{"true"},
		RuntimeSecretRefs: map[string]string{"TOKEN": "runtime-token"},
	})
	apiErr, ok := AsError(err)
	if !ok || apiErr.Code != "execution_secret_unavailable" {
		t.Fatalf("err=%v api=%+v", err, apiErr)
	}
	_, err = service.StartPreparedOnRunner(context.Background(), "project-1", "session-1", AuthorizedExecutionRequest{
		Command:           []string{"true"},
		RuntimeSecretRefs: map[string]string{"TOKEN": "runtime-token"},
	})
	apiErr, ok = AsError(err)
	if !ok || apiErr.Code != "execution_secret_unavailable" {
		t.Fatalf("prepared err=%v api=%+v", err, apiErr)
	}
}

func TestAuthorizedCreateAndStartPreparedRunnerSession(t *testing.T) {
	transport := newFakeExecutionTransport("session-1")
	client := &fakeExecutionClient{transport: transport, done: make(chan struct{})}
	manager := &fakeExecutionManager{client: client}
	storeFake := &executionSessionStoreFake{
		run: store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
	}
	lowLevel, err := NewExecutionSessionService(storeFake, manager, manager)
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
	if err != nil || session.RunnerID != "runner-1" || session.RuntimeInstanceID != "" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	process, err := service.StartPreparedOnRunner(context.Background(), "project-1", session.ID, AuthorizedExecutionRequest{Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	if process.Record().RunnerID != "runner-1" || process.Record().RuntimeInstanceID != "" {
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

func TestAuthorizedExecutionAttachReusesLiveSession(t *testing.T) {
	lowLevel, storeFake, transport := executionServiceFixture(t)
	service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(t.Context(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{Command: []string{"sleep", "10"}})
	if err != nil {
		t.Fatal(err)
	}
	attached, err := service.Attach(t.Context(), "project-1", started.ID())
	if err != nil {
		t.Fatal(err)
	}
	if attached.ID() != started.ID() {
		t.Fatalf("attached=%q started=%q", attached.ID(), started.ID())
	}
	if storeFake.session.ID != started.ID() {
		t.Fatalf("attach created a new session: %+v", storeFake.session)
	}
	close(transport.resultCh)
}

func TestAuthorizedExecutionAttachPreparesAuthorizedSecretRedaction(t *testing.T) {
	lowLevel, _, transport := executionServiceFixture(t)
	preparer := &fakeExecutionPreparer{prepared: executioncontext.Prepared{}}
	service, err := NewAuthorizedExecutionSessionService(lowLevel, preparer)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(t.Context(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{
		Command:               []string{"sleep", "10"},
		ProviderCredentialEnv: "PROVIDER_TOKEN",
		RuntimeSecretRefs:     map[string]string{"RUNTIME_TOKEN": "runtime-token"},
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
		lowLevel, storeFake, transport := executionServiceFixture(t)
		service, err := NewAuthorizedExecutionSessionService(lowLevel, &fakeExecutionPreparer{})
		if err != nil {
			t.Fatal(err)
		}
		started, err := service.Start(t.Context(), "project-1", "run-1", "runtime-1", AuthorizedExecutionRequest{Command: []string{"sleep", "10"}})
		if err != nil {
			t.Fatal(err)
		}
		storeFake.session.Status = "COMPLETED"
		_, err = service.Attach(t.Context(), "project-1", started.ID())
		apiErr, ok := AsError(err)
		if !ok || apiErr.Code != "execution_session_not_running" {
			t.Fatalf("err=%v api=%+v", err, apiErr)
		}
		close(transport.resultCh)
	})
}

type requestCapturingClient struct {
	*fakeExecutionClient
	request runner.Request
}

func (c *requestCapturingClient) Start(ctx context.Context, sessionID string, request runner.Request) (runner.ProcessSession, error) {
	c.request = request
	return c.fakeExecutionClient.Start(ctx, sessionID, request)
}
