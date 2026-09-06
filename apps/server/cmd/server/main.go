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
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/httpapi"
	"github.com/brantje/agent-board/apps/server/internal/repository"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	dockerruntime "github.com/brantje/agent-board/apps/server/internal/runtime/docker"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

const (
	defaultAddress       = ":3001"
	defaultWorkspaceRoot = "/var/lib/agent-board/workspaces"
	shutdownTimeout      = 10 * time.Second
	serverReadTimeout    = 30 * time.Second
	serverIdleTimeout    = 60 * time.Second
)

type applicationHandler struct {
	http.Handler
	services *app.Services
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	handler, closeStore, err := controlPlaneHandler(ctx, os.Getenv("AGENT_BOARD_DATABASE_URL"))
	if err != nil {
		slog.Error("initialize control plane", "error", err)
		stop()
		os.Exit(1)
	}
	if err := reconcileRuntimeInstances(ctx, handler); err != nil {
		slog.Error("reconcile Runtime Instances", "error", err)
		closeStore()
		stop()
		os.Exit(1)
	}
	if err := reconcileExecutionSessions(ctx, handler); err != nil {
		slog.Error("reconcile Execution Sessions", "error", err)
		closeStore()
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
	services, err := configuredApplication(database)
	if err != nil {
		database.Close()
		return nil, nil, fmt.Errorf("configure application: %w", err)
	}
	closeApplication := func() {
		if err := services.Close(); err != nil {
			slog.Error("close application services", "error", err)
		}
		database.Close()
	}
	return &applicationHandler{
		Handler:  httpapi.NewRouter(services.ControlPlane),
		services: services,
	}, closeApplication, nil
}

func reconcileRuntimeInstances(ctx context.Context, handler http.Handler) error {
	application, ok := handler.(*applicationHandler)
	if !ok || application.services == nil || application.services.RuntimeInstances == nil {
		return fmt.Errorf("runtime instance service is unavailable")
	}
	return application.services.RuntimeInstances.ReconcileAllWithReporter(ctx, func(err error) {
		slog.Error("reconcile Runtime Instance", "error", err)
	})
}

func reconcileExecutionSessions(ctx context.Context, handler http.Handler) error {
	application, ok := handler.(*applicationHandler)
	if !ok || application.services == nil || application.services.ExecutionSessions == nil {
		return fmt.Errorf("execution session service is unavailable")
	}
	return application.services.ExecutionSessions.ReconcileAllWithReporter(ctx, func(err error) {
		slog.Error("reconcile Execution Session", "error", err)
	})
}

func configuredApplication(database *postgres.Store) (*app.Services, error) {
	roots := repository.ParseRoots(os.Getenv("AGENT_BOARD_REPOSITORY_ROOTS"))
	if len(roots) == 0 {
		return nil, fmt.Errorf("repository roots: %w", repository.ErrNoAuthorizedRoots)
	}
	policy, err := repository.NewPolicy(roots)
	if err != nil {
		return nil, fmt.Errorf("repository roots: %w", err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		return nil, err
	}
	materializer, err := workspace.NewMaterializer(database, policy, git, configuredWorkspaceRoot())
	if err != nil {
		return nil, err
	}
	dockerRuntime, err := dockerruntime.New()
	if err != nil {
		return nil, err
	}
	secretResolver, err := configuredSecretResolver(database)
	if err != nil {
		_ = dockerRuntime.Close()
		return nil, err
	}
	services, err := app.NewServicesWithRuntimes(database, materializer, map[string]runtimepkg.Implementation{"docker": dockerRuntime}, secretResolver)
	if err != nil {
		_ = dockerRuntime.Close()
		return nil, err
	}
	return services, nil
}

func configuredSecretResolver(database *postgres.Store) (executioncontext.SecretResolver, error) {
	rawKey := os.Getenv("AGENT_BOARD_SECRET_ENCRYPTION_KEY")
	if rawKey == "" {
		return nil, nil
	}
	key, err := secrets.ParseKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("secret encryption key: %w", err)
	}
	cipher, err := secrets.NewAESGCM(1, map[int][]byte{1: key})
	if err != nil {
		return nil, fmt.Errorf("secret encryption cipher: %w", err)
	}
	service, err := secrets.NewService(database, cipher)
	if err != nil {
		return nil, fmt.Errorf("secret resolver: %w", err)
	}
	return service, nil
}

func configuredWorkspaceRoot() string {
	if root := os.Getenv("AGENT_BOARD_WORKSPACE_ROOT"); root != "" {
		return root
	}
	return defaultWorkspaceRoot
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
