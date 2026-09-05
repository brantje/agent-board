package session

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestWaitCompletesWithoutConsumingProcessOutput(t *testing.T) {
	manager := NewManagerWithWorkspace(1, t.TempDir())
	execution, err := manager.Start("unread-output", Request{
		Command: []string{"sh", "-c", "dd if=/dev/zero bs=65536 count=32 2>/dev/null; printf stderr-marker >&2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := execution.Wait(ctx)
	if err != nil {
		t.Fatalf("wait should not depend on stream consumption: %v", err)
	}
	if result.ExitCode != 0 || result.Signaled {
		t.Fatalf("unexpected result %#v", result)
	}
	waitFor(t, time.Second, func() bool { return manager.ActiveCount() == 0 })

	stdout, err := io.ReadAll(execution.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if len(stdout) != 2*1024*1024 {
		t.Fatalf("unexpected stdout length %d", len(stdout))
	}
	stderr, err := io.ReadAll(execution.Stderr())
	if err != nil {
		t.Fatal(err)
	}
	if string(stderr) != "stderr-marker" {
		t.Fatalf("unexpected stderr %q", stderr)
	}
}
