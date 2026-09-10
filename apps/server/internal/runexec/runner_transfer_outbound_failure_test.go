package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runnerTransferFailureClient struct {
	sendErr error
	payload []byte
}

func (c *runnerTransferFailureClient) SendTransfer(context.Context, string, string, string, []byte, runner.TransferProgressFunc) error {
	return c.sendErr
}

func (c *runnerTransferFailureClient) ReceiveTransfer(context.Context, string, runner.TransferProgressFunc) (string, []byte, error) {
	return "returned-transfer", c.payload, nil
}

func (c *runnerTransferFailureClient) ConfirmTransferApplied(context.Context, string, string) error {
	return nil
}

type runnerTransferFailingConnector struct{ err error }

func (c runnerTransferFailingConnector) Connect(context.Context, string, string) (runnerClient, error) {
	return nil, c.err
}

type runnerTransferLockFailureStore struct {
	*runnerSyncStore
	err error
}

func (s *runnerTransferLockFailureStore) AcquireWorkspaceExecutionLock(context.Context, string, string) (store.WorkspaceBootstrapLock, error) {
	return nil, s.err
}

func newRunnerTransferFailureProcessor(t *testing.T, client runnerClient) (*Processor, executioncontext.SafeContext, *runnerSyncStore) {
	t.Helper()
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	storeFake := &runnerSyncStore{}
	return newRunnerSyncProcessor(t, repo, safe, storeFake, client), safe, storeFake
}

func TestTransferWorkspaceToRunnerFailsAtExternalBoundaries(t *testing.T) {
	boundaryErr := errors.New("boundary failed")

	t.Run("connector unavailable", func(t *testing.T) {
		processor, safe, _ := newRunnerTransferFailureProcessor(t, &runnerTransferFailureClient{})
		processor.runners = nil
		if err := processor.transferWorkspaceToRunner(t.Context(), safe, "runner-1", "session-1"); err == nil {
			t.Fatal("transfer succeeded without runner connector")
		}
	})

	t.Run("workspace lock", func(t *testing.T) {
		processor, safe, storeFake := newRunnerTransferFailureProcessor(t, &runnerTransferFailureClient{})
		processor.store = &runnerTransferLockFailureStore{runnerSyncStore: storeFake, err: boundaryErr}
		if err := processor.transferWorkspaceToRunner(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, boundaryErr) {
			t.Fatalf("lock error=%v", err)
		}
	})

	t.Run("runner connection", func(t *testing.T) {
		processor, safe, _ := newRunnerTransferFailureProcessor(t, &runnerTransferFailureClient{})
		processor.runners = runnerTransferFailingConnector{err: boundaryErr}
		if err := processor.transferWorkspaceToRunner(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, boundaryErr) {
			t.Fatalf("connect error=%v", err)
		}
	})

	t.Run("runner send", func(t *testing.T) {
		processor, safe, _ := newRunnerTransferFailureProcessor(t, &runnerTransferFailureClient{sendErr: boundaryErr})
		if err := processor.transferWorkspaceToRunner(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, boundaryErr) {
			t.Fatalf("send error=%v", err)
		}
	})
}
