package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	runnerserver "github.com/brantje/agent-board/apps/agent-runner/internal/server"
	"github.com/brantje/agent-board/apps/agent-runner/internal/session"
)

const defaultListenAddr = ":8080"

type appConfig struct {
	ListenAddr    string
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
	listenAddr := os.Getenv("AGENT_RUNNER_ADDR")
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	workspaceRoot := os.Getenv("AGENT_RUNNER_WORKSPACE_ROOT")
	if workspaceRoot == "" {
		workspaceRoot = session.DefaultWorkspaceRoot
	}
	return appConfig{ListenAddr: listenAddr, WorkspaceRoot: workspaceRoot}
}

func run(ctx context.Context, config appConfig) error {
	handler := runnerserver.New(runnerserver.Config{
		WorkspaceRoot:     config.WorkspaceRoot,
		MaxActiveSessions: 1,
	})
	httpServer := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpErr := httpServer.Shutdown(shutdownCtx)
		runnerErr := handler.Shutdown(shutdownCtx)
		if err := joinShutdownErrors(httpErr, runnerErr); err != nil {
			return err
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func joinShutdownErrors(httpErr, runnerErr error) error {
	return errors.Join(httpErr, runnerErr)
}
