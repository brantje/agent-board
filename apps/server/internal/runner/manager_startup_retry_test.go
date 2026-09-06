package runner

import (
	"context"
	"fmt"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestDialRunnerStartupRetriesConnectionRefused(t *testing.T) {
	client := newFakeManagerClient()
	var calls atomic.Int32

	got, err := dialRunnerStartup(t.Context(), func(context.Context, string) (Client, error) {
		if calls.Add(1) < 3 {
			return nil, fmt.Errorf("runner not listening yet: %w", syscall.ECONNREFUSED)
		}
		return client, nil
	}, "ws://runner.test/v1/ws")
	if err != nil {
		t.Fatalf("dialRunnerStartup() error = %v", err)
	}
	if got != client {
		t.Fatalf("dialRunnerStartup() client = %v, want %v", got, client)
	}
	if calls.Load() != 3 {
		t.Fatalf("dial attempts = %d, want 3", calls.Load())
	}
}

func TestDialRunnerStartupDoesNotRetryNonStartupFailure(t *testing.T) {
	want := fmt.Errorf("protocol mismatch")
	var calls atomic.Int32
	_, err := dialRunnerStartup(t.Context(), func(context.Context, string) (Client, error) {
		calls.Add(1)
		return nil, want
	}, "ws://runner.test/v1/ws")
	if err != want {
		t.Fatalf("dialRunnerStartup() error = %v, want %v", err, want)
	}
	if calls.Load() != 1 {
		t.Fatalf("dial attempts = %d, want 1", calls.Load())
	}
}
