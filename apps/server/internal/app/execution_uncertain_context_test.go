package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type contextAwareRunnerStatusStore struct {
	*branchExecutionStore
	usedLiveContext bool
}

func (s *contextAwareRunnerStatusStore) UpdateRuntimeInstanceRunnerStatus(ctx context.Context, projectID, instanceID, status string) (store.RuntimeInstance, error) {
	if err := ctx.Err(); err != nil {
		return store.RuntimeInstance{}, err
	}
	s.usedLiveContext = true
	return s.branchExecutionStore.UpdateRuntimeInstanceRunnerStatus(ctx, projectID, instanceID, status)
}

type cancelOnStartClient struct {
	*fakeExecutionClient
	cancel context.CancelFunc
}

func (c *cancelOnStartClient) Start(ctx context.Context, sessionID string, request runner.Request) (runner.ProcessSession, error) {
	c.cancel()
	<-ctx.Done()
	return c.transport, ctx.Err()
}

func TestUncertainStartPersistsBusyAfterCallerContextExpires(t *testing.T) {
	baseStore := newBranchExecutionStore()
	storeFake := &contextAwareRunnerStatusStore{branchExecutionStore: baseStore}
	transport := newFakeExecutionTransport("session-1")
	ctx, cancel := context.WithCancel(context.Background())
	client := &cancelOnStartClient{
		fakeExecutionClient: &fakeExecutionClient{transport: transport, done: make(chan struct{})},
		cancel:              cancel,
	}
	service, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Start(ctx, "project-1", "run-1", "runtime-1", ExecutionRequest{Command: []string{"true"}}); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error=%v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("caller context did not expire")
	}
	if !storeFake.usedLiveContext || storeFake.instance.RunnerStatus != "BUSY" {
		t.Fatalf("usedLiveContext=%v instance=%+v", storeFake.usedLiveContext, storeFake.instance)
	}

	close(transport.resultCh)
}
