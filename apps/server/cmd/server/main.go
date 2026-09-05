package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/httpapi"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
)

const (
	defaultAddress    = ":3001"
	shutdownTimeout   = 10 * time.Second
	serverReadTimeout = 30 * time.Second
	serverIdleTimeout = 60 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	handler, closeStore, err := controlPlaneHandler(ctx, os.Getenv("AGENT_BOARD_DATABASE_URL"))
	if err != nil {
		slog.Error("initialize control plane", "error", err)
		stop()
		os.Exit(1)
	}
	code := exitCode(ctx, configuredAddress(), handler)
	closeStore()
	stop()
	os.Exit(code)
}

func controlPlaneHandler(ctx context.Context, databaseURL string) (http.Handler, func(), error) {
	if databaseURL == "" {
		return nil, nil, fmt.Errorf("AGENT_BOARD_DATABASE_URL is required")
	}
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	return httpapi.NewRouter(app.New(database)), database.Close, nil
}

func exitCode(ctx context.Context, address string, handlers ...http.Handler) int {
	if err := run(ctx, address, handlers...); err != nil {
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

func newHTTPServer(address string, handlers ...http.Handler) *http.Server {
	handler := httpapi.NewRouter()
	if len(handlers) > 0 && handlers[0] != nil {
		handler = handlers[0]
	}
	return &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: serverReadTimeout, IdleTimeout: serverIdleTimeout}
}

func run(ctx context.Context, address string, handlers ...http.Handler) error {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	return serve(ctx, newHTTPServer(address, handlers...), listener)
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
