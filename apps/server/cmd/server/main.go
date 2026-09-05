package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/httpapi"
)

const (
	defaultAddress     = ":3001"
	shutdownTimeout    = 10 * time.Second
	serverReadTimeout  = 30 * time.Second
	serverIdleTimeout  = 60 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(exitCode(ctx, configuredAddress()))
}

func exitCode(ctx context.Context, address string) int {
	if err := run(ctx, address); err != nil {
		slog.Error("server stopped", "error", err)
		return 1
	}
	return 0
}

func configuredAddress() string {
	if address := os.Getenv("AGENT_BOARD_SERVER_ADDR"); address != "" {
		return address
	}
	return defaultAddress
}

func newHTTPServer(address string) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           httpapi.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       serverReadTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
}

func run(ctx context.Context, address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()

	return serve(ctx, newHTTPServer(address), listener)
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("starting Agent Board server", "address", listener.Addr().String())
		serverErrors <- server.Serve(listener)
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
