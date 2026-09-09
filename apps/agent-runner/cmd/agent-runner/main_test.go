package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("AGENT_BOARD_URL", "http://127.0.0.1:9876")
	t.Setenv("AGENT_RUNNER_ID", "runner")
	t.Setenv("AGENT_RUNNER_TOKEN", "token")
	t.Setenv("AGENT_RUNNER_WORKSPACE_ROOT", "/tmp/workspace")
	config := configFromEnv()
	if config.ServerURL != "http://127.0.0.1:9876" || config.RunnerID != "runner" || config.Token != "token" || config.WorkspaceRoot != "/tmp/workspace" {
		t.Fatalf("unexpected config %#v", config)
	}
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("AGENT_BOARD_URL", "")
	t.Setenv("AGENT_RUNNER_WORKSPACE_ROOT", "")
	config := configFromEnv()
	if config.ServerURL != "" || config.WorkspaceRoot != defaultWorkspaceRoot {
		t.Fatalf("unexpected defaults %#v", config)
	}
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- run(ctx, appConfig{ServerURL: "http://127.0.0.1:1", RunnerID: "runner", Token: "token", WorkspaceRoot: t.TempDir()})
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

func TestRunRejectsInvalidConnectionConfiguration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx, appConfig{ServerURL: "invalid address", WorkspaceRoot: t.TempDir()}); err == nil {
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
