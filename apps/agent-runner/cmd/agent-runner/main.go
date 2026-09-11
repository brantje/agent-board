package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	runnerserver "github.com/brantje/agent-board/apps/agent-runner/internal/server"
)

const defaultWorkspaceRoot = "/var/lib/agent-runner/workspaces"
const defaultMaxActiveSessions = 10

type appConfig struct {
	ServerURL         string
	RunnerID          string
	Token             string
	WorkspaceRoot     string
	MaxActiveSessions int
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := execute(ctx, os.Args, &http.Client{Timeout: 30 * time.Second}, os.Stdin, os.Stdout, defaultConfigPath); err != nil {
		slog.Error("agent-runner stopped", "error", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, client httpDoer, input io.Reader, output io.Writer, configPath string) error {
	if len(args) > 1 {
		if args[1] != "register" || len(args) != 2 {
			return errors.New("usage: agent-runner [register]")
		}
		return registerInteractive(ctx, client, input, output, configPath)
	}
	config := configFromEnv()
	if config.ServerURL == "" || config.RunnerID == "" || config.Token == "" {
		return errors.New("runner is not registered; run `agent-runner register`")
	}
	return run(ctx, config)
}

func configFromEnv() appConfig {
	root := os.Getenv("AGENT_RUNNER_WORKSPACE_ROOT")
	if root == "" {
		root = defaultWorkspaceRoot
	}
	return appConfig{ServerURL: os.Getenv("AGENT_BOARD_URL"), RunnerID: os.Getenv("AGENT_RUNNER_ID"), Token: os.Getenv("AGENT_RUNNER_TOKEN"), WorkspaceRoot: root, MaxActiveSessions: defaultMaxActiveSessions}
}

func run(ctx context.Context, config appConfig) error {
	sessions := config.MaxActiveSessions
	if sessions < 1 {
		sessions = defaultMaxActiveSessions
	}
	handler := runnerserver.New(runnerserver.Config{WorkspaceRoot: config.WorkspaceRoot, MaxActiveSessions: sessions})
	connectionErr := handler.Connect(ctx, config.ServerURL, config.RunnerID, config.Token)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return joinShutdownErrors(connectionErr, handler.Shutdown(shutdownCtx))
}

func joinShutdownErrors(httpErr, runnerErr error) error {
	return errors.Join(httpErr, runnerErr)
}
