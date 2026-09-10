package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
)

type runnerSyncFailureClient struct {
	sendErr    error
	receiveErr error
	payload    []byte
}

func (c *runnerSyncFailureClient) SendTransfer(context.Context, string, string, string, []byte, runner.TransferProgressFunc) error {
	return c.sendErr
}

func (c *runnerSyncFailureClient) ReceiveTransfer(context.Context, string, runner.TransferProgressFunc) (string, []byte, error) {
	if c.receiveErr != nil {
		return "", nil, c.receiveErr
	}
	return "returned-transfer", c.payload, nil
}

func (c *runnerSyncFailureClient) ConfirmTransferApplied(context.Context, string, string) error {
	return nil
}

func TestSyncWorkspaceFromRunnerFailsBeforeAuthoritativeCompletion(t *testing.T) {
	boundaryErr := errors.New("boundary failed")

	t.Run("prepared session required", func(t *testing.T) {
		processor, safe, _ := newRunnerTransferFailureProcessor(t, &runnerSyncFailureClient{})
		if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", ""); err == nil {
			t.Fatal("sync succeeded without prepared runner session")
		}
	})

	t.Run("runner connection", func(t *testing.T) {
		processor, safe, _ := newRunnerTransferFailureProcessor(t, &runnerSyncFailureClient{})
		processor.runners = runnerTransferFailingConnector{err: boundaryErr}
		if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, boundaryErr) {
			t.Fatalf("connect error=%v", err)
		}
	})

	t.Run("transfer request", func(t *testing.T) {
		processor, safe, _ := newRunnerTransferFailureProcessor(t, &runnerSyncFailureClient{sendErr: boundaryErr})
		if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, boundaryErr) {
			t.Fatalf("send error=%v", err)
		}
	})

	t.Run("transfer receive", func(t *testing.T) {
		processor, safe, _ := newRunnerTransferFailureProcessor(t, &runnerSyncFailureClient{receiveErr: boundaryErr})
		if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, boundaryErr) {
			t.Fatalf("receive error=%v", err)
		}
	})

	t.Run("workspace lock", func(t *testing.T) {
		processor, safe, storeFake := newRunnerTransferFailureProcessor(t, &runnerSyncFailureClient{payload: []byte("unused")})
		processor.store = &runnerTransferLockFailureStore{runnerSyncStore: storeFake, err: boundaryErr}
		if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, boundaryErr) {
			t.Fatalf("lock error=%v", err)
		}
	})

	t.Run("invalid returned bundle", func(t *testing.T) {
		processor, safe, _ := newRunnerTransferFailureProcessor(t, &runnerSyncFailureClient{payload: []byte("not a git bundle")})
		if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); err == nil {
			t.Fatal("invalid runner workspace bundle was applied")
		}
	})
}
