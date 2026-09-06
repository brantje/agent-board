package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type branchExecutionStore struct {
	*executionSessionStoreFake
	getRunErr        error
	getInstanceErr   error
	createErr        error
	transitionErr    error
	updateRunnerErr  error
	mutateTransition bool
}

func (s *branchExecutionStore) GetRun(ctx context.Context, projectID, runID string) (store.Run, error) {
	if s.getRunErr != nil {
		return store.Run{}, s.getRunErr
	}
	return s.executionSessionStoreFake.GetRun(ctx, projectID, runID)
}

func (s *branchExecutionStore) GetRuntimeInstance(ctx context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	if s.getInstanceErr != nil {
		return store.RuntimeInstance{}, s.getInstanceErr
	}
	return s.executionSessionStoreFake.GetRuntimeInstance(ctx, projectID, instanceID)
}

func (s *branchExecutionStore) CreateExecutionSession(ctx context.Context, input store.ExecutionSession) (store.ExecutionSession, error) {
	if s.createErr != nil {
		return store.ExecutionSession{}, s.createErr
	}
	return s.executionSessionStoreFake.CreateExecutionSession(ctx, input)
}

func (s *branchExecutionStore) TransitionExecutionSession(ctx context.Context, transition store.ExecutionSessionTransition) (store.ExecutionSession, error) {
	if s.transitionErr != nil {
		return store.ExecutionSession{}, s.transitionErr
	}
	updated, err := s.executionSessionStoreFake.TransitionExecutionSession(ctx, transition)
	if err == nil && s.mutateTransition {
		updated.RunID = "mutated-run"
	}
	return updated, err
}

func (s *branchExecutionStore) UpdateRuntimeInstanceRunnerStatus(ctx context.Context, projectID, instanceID, status string) (store.RuntimeInstance, error) {
	if s.updateRunnerErr != nil {
		return store.RuntimeInstance{}, s.updateRunnerErr
	}
	return s.executionSessionStoreFake.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, instanceID, status)
}

func newBranchExecutionStore() *branchExecutionStore {
	return &branchExecutionStore{executionSessionStoreFake: &executionSessionStoreFake{
		run:      store.Run{ID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"},
		instance: store.RuntimeInstance{ID: "runtime-1", ProjectID: "project-1", WorkspaceID: "workspace-1", RuntimeID: "runtime-config-1", Status: "RUNNING", RunnerStatus: "READY"},
	}}
}

func newBranchExecutionService(t *testing.T, sessionStore ExecutionSessionStore, transport runner.ProcessSession, startErr error) *ExecutionSessionService {
	t.Helper()
	client := &fakeExecutionClient{transport: transport, startErr: startErr, done: make(chan struct{})}
	service, err := NewExecutionSessionService(sessionStore, &fakeExecutionManager{client: client})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestExecutionSessionConstructorAndValidationBranches(t *testing.T) {
	manager := &fakeExecutionManager{}
	storeFake := newBranchExecutionStore()
	if _, err := NewExecutionSessionService(nil, manager); err == nil {
		t.Fatal("expected nil store rejection")
	}
	if _, err := NewExecutionSessionService(storeFake, nil); err == nil {
		t.Fatal("expected nil manager rejection")
	}

	service := newBranchExecutionService(t, storeFake, newFakeExecutionTransport("session-1"), nil)
	cases := []ExecutionRequest{
		{},
		{Command: []string{""}},
		{Command: []string{"true"}, CWD: "/tmp"},
		{Command: []string{"true"}, CWD: "/workspace/../tmp"},
	}
	for _, request := range cases {
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", request); err == nil {
			t.Fatalf("Start(%+v) unexpectedly succeeded", request)
		}
	}
	if _, err := service.Start(context.Background(), "", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
		t.Fatal("expected blank project rejection")
	}
	if cloneMap(nil) != nil {
		t.Fatal("cloneMap(nil) must stay nil")
	}
	original := map[string]string{"A": "one"}
	cloned := cloneMap(original)
	cloned["A"] = "two"
	if original["A"] != "one" {
		t.Fatal("cloneMap aliased input")
	}
}

func TestExecutionSessionStartFailureBranches(t *testing.T) {
	t.Run("run lookup", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.getRunErr = store.ErrNotFound
		service := newBranchExecutionService(t, storeFake, newFakeExecutionTransport("session-1"), nil)
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
			t.Fatal("expected run lookup error")
		}
	})

	t.Run("runtime lookup", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.getInstanceErr = store.ErrNotFound
		service := newBranchExecutionService(t, storeFake, newFakeExecutionTransport("session-1"), nil)
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
			t.Fatal("expected runtime lookup error")
		}
	})

	t.Run("runtime not running", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.instance.Status = "STOPPED"
		service := newBranchExecutionService(t, storeFake, newFakeExecutionTransport("session-1"), nil)
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); !errors.Is(err, runtimepkg.ErrRunnerUnavailable) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("create conflict", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.createErr = store.ErrConflict
		service := newBranchExecutionService(t, storeFake, newFakeExecutionTransport("session-1"), nil)
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
			t.Fatal("expected create conflict")
		}
	})

	t.Run("transition invariant", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.mutateTransition = true
		service := newBranchExecutionService(t, storeFake, newFakeExecutionTransport("session-1"), nil)
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil {
			t.Fatal("expected immutable binding error")
		}
	})

	t.Run("connect failure persists failed", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		service, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{err: errors.New("dial failed")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil || storeFake.session.Status != "FAILED" {
			t.Fatalf("session=%+v err=%v", storeFake.session, err)
		}
	})

	t.Run("protocol start failure persists failed", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		service := newBranchExecutionService(t, storeFake, newFakeExecutionTransport("session-1"), &runner.ProtocolError{Code: "rejected", Message: "no"})
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil || storeFake.session.Status != "FAILED" {
			t.Fatalf("session=%+v err=%v", storeFake.session, err)
		}
	})

	t.Run("transport start failure stays uncertain", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		service := newBranchExecutionService(t, storeFake, newFakeExecutionTransport("session-1"), runner.ErrDisconnected)
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil || storeFake.session.Status != "STARTING" {
			t.Fatalf("session=%+v err=%v", storeFake.session, err)
		}
	})

	t.Run("runner status persistence failure stays uncertain", func(t *testing.T) {
		storeFake := newBranchExecutionStore()
		storeFake.updateRunnerErr = errors.New("status write failed")
		service := newBranchExecutionService(t, storeFake, newFakeExecutionTransport("session-1"), nil)
		if _, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil || storeFake.session.Status != "RUNNING" {
			t.Fatalf("session=%+v err=%v", storeFake.session, err)
		}
	})
}

func TestExecutionProcessAccessorsWaitKillAndFailure(t *testing.T) {
	storeFake := newBranchExecutionStore()
	transport := newFakeExecutionTransport("session-1")
	transport.stdout = "out"
	transport.stderr = "err"
	service := newBranchExecutionService(t, storeFake, transport, nil)
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{
		Command: []string{"cat"},
		Env:     map[string]string{"A": "B"},
		Secrets: map[string]string{"TOKEN": "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stdout, _ := io.ReadAll(process.Stdout())
	stderr, _ := io.ReadAll(process.Stderr())
	if string(stdout) != "out" || string(stderr) != "err" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
	if _, err := process.Stdin().Write([]byte("input")); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := process.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error=%v", err)
	}
	if err := process.Kill(context.Background()); err != nil || !transport.killed {
		t.Fatalf("Kill() err=%v killed=%v", err, transport.killed)
	}
	transport.result = runner.Result{ExitCode: 137, Signaled: true}
	close(transport.resultCh)
	if _, err := process.Wait(context.Background()); err != nil || process.Record().Status != "CANCELLED" {
		t.Fatalf("record=%+v err=%v", process.Record(), err)
	}

	failureStore := newBranchExecutionStore()
	failureTransport := newFakeExecutionTransport("session-1")
	failureService := newBranchExecutionService(t, failureStore, failureTransport, nil)
	failureProcess, err := failureService.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"false"}})
	if err != nil {
		t.Fatal(err)
	}
	failureTransport.waitErr = errors.New("wait failed")
	close(failureTransport.resultCh)
	if _, err := failureProcess.Wait(context.Background()); err == nil || failureProcess.Record().Status != "FAILED" {
		t.Fatalf("record=%+v err=%v", failureProcess.Record(), err)
	}
}

type cancelBranchTransport struct {
	id           string
	terminateErr error
	killErr      error
	result       runner.Result
	done         chan struct{}
	closeOnce    sync.Once
}

func newCancelBranchTransport(id string) *cancelBranchTransport {
	return &cancelBranchTransport{id: id, done: make(chan struct{}), result: runner.Result{ExitCode: 137, Signaled: true}}
}

func (t *cancelBranchTransport) ID() string                { return t.id }
func (t *cancelBranchTransport) Stdout() io.Reader         { return strings.NewReader("") }
func (t *cancelBranchTransport) Stderr() io.Reader         { return strings.NewReader("") }
func (t *cancelBranchTransport) Stdin() io.WriteCloser     { return nopBuffer{} }
func (t *cancelBranchTransport) finish()                   { t.closeOnce.Do(func() { close(t.done) }) }
func (t *cancelBranchTransport) Terminate(context.Context) error { return t.terminateErr }
func (t *cancelBranchTransport) Kill(context.Context) error      { return t.killErr }
func (t *cancelBranchTransport) Wait(ctx context.Context) (runner.Result, error) {
	select {
	case <-t.done:
		return t.result, nil
	case <-ctx.Done():
		return runner.Result{}, ctx.Err()
	}
}

func startCancelBranchProcess(t *testing.T, transport *cancelBranchTransport) (*ExecutionSessionService, *ExecutionProcess) {
	t.Helper()
	storeFake := newBranchExecutionStore()
	service := newBranchExecutionService(t, storeFake, transport, nil)
	process, err := service.Start(context.Background(), "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"sleep", "30"}})
	if err != nil {
		t.Fatal(err)
	}
	return service, process
}

func TestCancelFailureAndContextBranches(t *testing.T) {
	service := newBranchExecutionService(t, newBranchExecutionStore(), newFakeExecutionTransport("session-1"), nil)
	if err := service.Cancel(context.Background(), "", "session-1", time.Millisecond); err == nil {
		t.Fatal("expected invalid argument")
	}

	terminalStore := newBranchExecutionStore()
	terminalStore.session = store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunID: "run-1", RuntimeInstanceID: "runtime-1", Status: "COMPLETED"}
	terminalService := newBranchExecutionService(t, terminalStore, newFakeExecutionTransport("session-1"), nil)
	if err := terminalService.Cancel(context.Background(), "project-1", "session-1", time.Millisecond); err != nil {
		t.Fatalf("terminal Cancel() error=%v", err)
	}

	terminateTransport := newCancelBranchTransport("session-1")
	terminateTransport.terminateErr = errors.New("terminate failed")
	terminateService, terminateProcess := startCancelBranchProcess(t, terminateTransport)
	if err := terminateService.Cancel(context.Background(), "project-1", "session-1", time.Millisecond); err == nil {
		t.Fatal("expected terminate uncertainty")
	}
	terminateTransport.finish()
	_, _ = terminateProcess.Wait(context.Background())

	contextTransport := newCancelBranchTransport("session-1")
	contextService, contextProcess := startCancelBranchProcess(t, contextTransport)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := contextService.Cancel(ctx, "project-1", "session-1", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("Cancel() error=%v", err)
	}
	contextTransport.finish()
	_, _ = contextProcess.Wait(context.Background())

	disconnectTransport := newCancelBranchTransport("session-1")
	disconnectTransport.killErr = runner.ErrDisconnected
	disconnectService, disconnectProcess := startCancelBranchProcess(t, disconnectTransport)
	if err := disconnectService.Cancel(context.Background(), "project-1", "session-1", time.Millisecond); err == nil {
		t.Fatal("expected disconnected kill uncertainty")
	}
	disconnectTransport.finish()
	_, _ = disconnectProcess.Wait(context.Background())

	killTransport := newCancelBranchTransport("session-1")
	killTransport.killErr = errors.New("kill failed")
	killService, killProcess := startCancelBranchProcess(t, killTransport)
	if err := killService.Cancel(context.Background(), "project-1", "session-1", time.Millisecond); err == nil {
		t.Fatal("expected kill error")
	}
	killTransport.finish()
	_, _ = killProcess.Wait(context.Background())
}

type runnerEndpointRuntime struct {
	*fakeRuntimeImplementation
	endpoint runtimepkg.RunnerEndpoint
	err      error
}

func (r *runnerEndpointRuntime) RunnerEndpoint(context.Context, runtimepkg.Handle) (runtimepkg.RunnerEndpoint, error) {
	return r.endpoint, r.err
}

type runnerStatusRuntimeStore struct {
	*runtimeServiceStore
	err           error
	mutateBinding bool
}

func (s *runnerStatusRuntimeStore) UpdateRuntimeInstanceRunnerStatus(_ context.Context, projectID, instanceID, status string) (store.RuntimeInstance, error) {
	if s.err != nil {
		return store.RuntimeInstance{}, s.err
	}
	if s.instance.ID != instanceID || s.instance.ProjectID != projectID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	s.instance.RunnerStatus = status
	updated := s.instance
	if s.mutateBinding {
		updated.Status = "STOPPED"
	}
	return updated, nil
}

func runtimeRunnerService(t *testing.T, implementation runtimepkg.Implementation, statusStore *runnerStatusRuntimeStore) *RuntimeInstanceService {
	t.Helper()
	projectID := "project-1"
	externalID := "container-1"
	statusStore.runtime = store.Runtime{ID: "runtime-1", ProjectID: &projectID, Kind: "docker", Image: "runner:test", Enabled: true}
	statusStore.instance = store.RuntimeInstance{
		ID: "instance-1", ProjectID: projectID, WorkspaceID: "workspace-1", RuntimeID: "runtime-1",
		Status: "RUNNING", RunnerStatus: "READY", ExternalID: &externalID, SafeHandleMetadata: store.EmptyObject,
	}
	service, err := NewRuntimeInstanceService(statusStore, &runtimeWorkspaceEnsurer{}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestRuntimeRunnerEndpointAndStatusBranches(t *testing.T) {
	implementation := &runnerEndpointRuntime{fakeRuntimeImplementation: &fakeRuntimeImplementation{}, endpoint: runtimepkg.RunnerEndpoint{URL: "ws://runner:8765/ws"}}
	statusStore := &runnerStatusRuntimeStore{runtimeServiceStore: &runtimeServiceStore{}}
	service := runtimeRunnerService(t, implementation, statusStore)
	endpoint, err := service.RunnerEndpoint(context.Background(), "project-1", "instance-1")
	if err != nil || endpoint.URL != implementation.endpoint.URL {
		t.Fatalf("endpoint=%+v err=%v", endpoint, err)
	}
	if err := service.SetRunnerStatus(context.Background(), "project-1", "instance-1", "BUSY"); err != nil || statusStore.instance.RunnerStatus != "BUSY" {
		t.Fatalf("instance=%+v err=%v", statusStore.instance, err)
	}

	statusStore.instance.Status = "STOPPED"
	if _, err := service.RunnerEndpoint(context.Background(), "project-1", "instance-1"); !errors.Is(err, runtimepkg.ErrRunnerUnavailable) {
		t.Fatalf("error=%v", err)
	}
	statusStore.instance.Status = "RUNNING"

	unsupportedStore := &runnerStatusRuntimeStore{runtimeServiceStore: &runtimeServiceStore{}}
	unsupported := runtimeRunnerService(t, &fakeRuntimeImplementation{}, unsupportedStore)
	if _, err := unsupported.RunnerEndpoint(context.Background(), "project-1", "instance-1"); !errors.Is(err, runtimepkg.ErrUnsupportedPolicy) {
		t.Fatalf("error=%v", err)
	}

	providerErr := errors.New("endpoint failed")
	implementation.err = providerErr
	if _, err := service.RunnerEndpoint(context.Background(), "project-1", "instance-1"); !errors.Is(err, providerErr) {
		t.Fatalf("error=%v", err)
	}
	implementation.err = nil

	statusStore.err = store.ErrNotFound
	if err := service.SetRunnerStatus(context.Background(), "project-1", "instance-1", "READY"); err == nil {
		t.Fatal("expected runner status store error")
	}
	statusStore.err = nil
	statusStore.mutateBinding = true
	if err := service.SetRunnerStatus(context.Background(), "project-1", "instance-1", "READY"); err == nil {
		t.Fatal("expected lifecycle mutation rejection")
	}

	baseStore := &runtimeServiceStore{runtime: statusStore.runtime, instance: statusStore.instance}
	withoutStatus, err := NewRuntimeInstanceService(baseStore, &runtimeWorkspaceEnsurer{}, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}
	if err := withoutStatus.SetRunnerStatus(context.Background(), "project-1", "instance-1", "READY"); err == nil {
		t.Fatal("expected unsupported status store")
	}
}
