package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/session"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("AGENT_RUNNER_ADDR", "127.0.0.1:9876")
	t.Setenv("AGENT_RUNNER_WORKSPACE_ROOT", "/tmp/workspace")
	config := configFromEnv()
	if config.ListenAddr != "127.0.0.1:9876" || config.WorkspaceRoot != "/tmp/workspace" {
		t.Fatalf("unexpected config %#v", config)
	}
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("AGENT_RUNNER_ADDR", "")
	t.Setenv("AGENT_RUNNER_WORKSPACE_ROOT", "")
	config := configFromEnv()
	if config.ListenAddr != defaultListenAddr || config.WorkspaceRoot != session.DefaultWorkspaceRoot {
		t.Fatalf("unexpected defaults %#v", config)
	}
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- run(ctx, appConfig{ListenAddr: "127.0.0.1:0", WorkspaceRoot: t.TempDir()})
	}()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop after cancellation")
	}
}

func TestRunReportsListenFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx, appConfig{ListenAddr: "invalid address", WorkspaceRoot: t.TempDir()}); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestJoinShutdownErrorsPreservesBothErrors(t *testing.T) {
	httpErr := errors.New("http shutdown failed")
	runnerErr := errors.New("runner shutdown failed")
	err := joinShutdownErrors(httpErr, runnerErr)
	if !errors.Is(err, httpErr) {
		t.Fatalf("joined error does not preserve HTTP error: %v", err)
	}
	if !errors.Is(err, runnerErr) {
		t.Fatalf("joined error does not preserve runner error: %v", err)
	}
	if err := joinShutdownErrors(nil, nil); err != nil {
		t.Fatalf("expected nil when both shutdown errors are nil, got %v", err)
	}
}
