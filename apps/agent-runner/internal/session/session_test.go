package session

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionLifecycle(t *testing.T) {
	workspace := t.TempDir()
	manager := NewManagerWithWorkspace(1, workspace)
	s, err := manager.Start("session-1", Request{Command: []string{"sh", "-c", "read line; printf 'out:%s' \"$line\"; printf 'err:%s' \"$TOKEN\" >&2; exit 7"}, Env: map[string]string{"TOKEN": "env"}, Secrets: map[string]string{"TOKEN": "secret"}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := io.WriteString(s.Stdin(), "hello\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(s.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(s.Stderr())
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(stdout) != "out:hello" || string(stderr) != "err:***" {
		t.Fatalf("unexpected output stdout=%q stderr=%q", stdout, stderr)
	}
	if result.ExitCode != 7 || result.Signaled {
		t.Fatalf("unexpected result %#v", result)
	}
}

func TestManagerCapacityAndSequentialSessions(t *testing.T) {
	manager := NewManagerWithWorkspace(1, t.TempDir())
	first, err := manager.Start("first", Request{Command: []string{"sh", "-c", "sleep 1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start("second", Request{Command: []string{"true"}}); !errors.Is(err, ErrCapacityReached) {
		t.Fatalf("expected capacity error, got %v", err)
	}
	if _, err := manager.Start("first", Request{Command: []string{"true"}}); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if err := first.Kill(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return manager.ActiveCount() == 0 })

	second, err := manager.Start("second", Request{Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceBoundary(t *testing.T) {
	workspace := t.TempDir()
	subdir := filepath.Join(workspace, "sub")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManagerWithWorkspace(1, workspace)

	s, err := manager.Start("inside", Request{Command: []string{"pwd"}, Dir: "sub"})
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(s.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != subdir {
		t.Fatalf("unexpected working directory %q", output)
	}

	waitFor(t, time.Second, func() bool { return manager.ActiveCount() == 0 })
	if _, err := manager.Start("escape", Request{Command: []string{"true"}, Dir: ".."}); err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("expected workspace escape error, got %v", err)
	}
}

func TestWorkspaceBoundaryRejectsSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "outside")); err != nil {
		t.Fatal(err)
	}
	manager := NewManagerWithWorkspace(1, workspace)
	if _, err := manager.Start("escape", Request{Command: []string{"true"}, Dir: "outside"}); err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("expected symlink escape error, got %v", err)
	}
}

func TestSequentialSessionsReuseWorkspaceWithoutEnvironmentLeak(t *testing.T) {
	workspace := t.TempDir()
	manager := NewManagerWithWorkspace(1, workspace)
	secretName := "AGENT_BOARD_TEST_SESSION_SECRET_9D5A"

	first, err := manager.Start("first", Request{
		Command: []string{"sh", "-c", "printf '%s' \"$" + secretName + "\" > state"},
		Secrets: map[string]string{secretName: "ephemeral"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return manager.ActiveCount() == 0 })

	second, err := manager.Start("second", Request{Command: []string{"sh", "-c", "printf '%s|%s' \"$(cat state)\" \"${" + secretName + "-unset}\""}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(second.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if string(output) != "***|unset" {
		t.Fatalf("workspace/env isolation mismatch: %q", output)
	}
	if strings.Contains(string(output), "ephemeral") {
		t.Fatalf("prior session secret leaked through reusable workspace: %q", output)
	}
}

func TestManagerLookup(t *testing.T) {
	manager := NewManagerWithWorkspace(1, t.TempDir())
	if _, err := manager.Get("missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestSessionValidationAndWaitCancellation(t *testing.T) {
	workspace := t.TempDir()
	manager := NewManagerWithWorkspace(0, workspace)
	if manager.Capacity() != 1 {
		t.Fatalf("expected minimum capacity 1, got %d", manager.Capacity())
	}
	if _, err := manager.Start("", Request{Command: []string{"true"}}); err == nil {
		t.Fatal("expected missing id error")
	}
	if _, err := manager.Start("bad", Request{}); err == nil || !strings.Contains(err.Error(), "command") {
		t.Fatalf("expected command error, got %v", err)
	}

	s, err := manager.Start("slow", Request{Command: []string{"sh", "-c", "sleep 5"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled wait, got %v", err)
	}
	if err := s.Kill(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Kill(); err != nil {
		t.Fatalf("idempotent kill failed: %v", err)
	}
}

func TestTerminateProcessTree(t *testing.T) {
	manager := NewManagerWithWorkspace(1, t.TempDir())
	s, err := manager.Start("tree", Request{Command: []string{"sh", "-c", "sleep 30 & wait"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Terminate(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := s.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Signaled {
		t.Fatalf("expected signaled result, got %#v", result)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
