package main

import (
	"context"
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
