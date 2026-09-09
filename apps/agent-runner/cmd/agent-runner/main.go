package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	runnerserver "github.com/brantje/agent-board/apps/agent-runner/internal/server"
)

const defaultWorkspaceRoot = "/var/lib/agent-runner/workspaces"

type appConfig struct {
	ServerURL     string
	RunnerID      string
	Token         string
	WorkspaceRoot string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, configFromEnv()); err != nil {
		slog.Error("agent-runner stopped", "error", err)
		os.Exit(1)
	}
}

func configFromEnv() appConfig {
	root := os.Getenv("AGENT_RUNNER_WORKSPACE_ROOT")
	if root == "" {
		root = defaultWorkspaceRoot
	}
	return appConfig{ServerURL: os.Getenv("AGENT_BOARD_URL"), RunnerID: os.Getenv("AGENT_RUNNER_ID"), Token: os.Getenv("AGENT_RUNNER_TOKEN"), WorkspaceRoot: root}
}

func run(ctx context.Context, config appConfig) error {
	handler := runnerserver.New(runnerserver.Config{WorkspaceRoot: config.WorkspaceRoot, MaxActiveSessions: 1})
	connectionErr := handler.Connect(ctx, config.ServerURL, config.RunnerID, config.Token)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return joinShutdownErrors(connectionErr, handler.Shutdown(shutdownCtx))
}

func joinShutdownErrors(httpErr, runnerErr error) error {
	return errors.Join(httpErr, runnerErr)
}
