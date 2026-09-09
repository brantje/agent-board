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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode"
	"github.com/brantje/agent-board/apps/server/internal/engine/scripted"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/httpapi"
	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/runexec"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	dockerruntime "github.com/brantje/agent-board/apps/server/internal/runtime/docker"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

const (
	defaultAddress           = ":3001"
	defaultWorkspaceRoot     = "/var/lib/agent-board/workspaces"
	defaultEvidenceRoot      = "/var/lib/agent-board/evidence"
	defaultEvidenceBlobLimit = 8 << 20
	defaultOutputChunkSize   = 64 << 10
	shutdownTimeout          = 10 * time.Second
	serverReadTimeout        = 30 * time.Second
	serverIdleTimeout        = 60 * time.Second
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
	if application, ok := handler.(*applicationHandler); ok && application.services != nil && application.services.Redaction != nil {
		baseHandler := slog.NewTextHandler(os.Stderr, nil)
		slog.SetDefault(slog.New(redaction.NewSlogHandler(baseHandler, application.services.Redaction)))
	}
	if application, ok := handler.(*applicationHandler); ok && application.services != nil {
		if err := app.EnsureOpenRouterFromEnv(ctx, application.services.ControlPlane, app.SecretStoreFromWriter(application.services.Secrets), os.Getenv); err != nil {
			slog.Error("bootstrap OpenRouter provider", "error", err)
			closeStore()
			stop()
			os.Exit(1)
		}
	}
	if err := reconcileRuntimeInstances(ctx, handler); err != nil {
		slog.Error("reconcile Runtime Instance", "error", err)
		closeStore()
		stop()
		os.Exit(1)
	}
	if err := reconcileExecutionSessions(ctx, handler); err != nil {
		slog.Error("reconcile Execution Session", "error", err)
		closeStore()
		stop()
		os.Exit(1)
	}
	schedulerDone, err := startScheduler(ctx, handler)
	if err != nil {
		slog.Error("start scheduler", "error", err)
		closeStore()
		stop()
		os.Exit(1)
	}
	internalDone := make(chan struct{})
	go func() {
		defer close(internalDone)
		application, ok := handler.(*applicationHandler)
		if !ok || application.services == nil || application.services.ControlPlane.Runners == nil {
			return
		}
		binary := os.Getenv("AGENT_BOARD_RUNNER_BINARY")
		if binary == "" {
			binary = "agent-runner"
		}
		host, port, err := net.SplitHostPort(configuredAddress())
		if err != nil {
			slog.Error("internal runner address is invalid")
			stop()
			return
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		endpoint := "http://" + net.JoinHostPort(host, port)
		root := filepath.Join(configuredWorkspaceRoot(), ".internal-runner")
		if err := application.services.ControlPlane.Runners.SuperviseInternalRunner(ctx, binary, endpoint, root); err != nil && ctx.Err() == nil {
			slog.Error("internal runner supervision failed", "error", err)
			stop()
		}
	}()
	code := supervise(ctx, stop, func() int {
		return exitCode(ctx, configuredAddress(), handler)
	}, schedulerDone)
	<-internalDone
	closeStore()
	os.Exit(code)
}

func supervise(ctx context.Context, cancel context.CancelFunc, serve func() int, schedulerDone <-chan error) int {
	serverDone := make(chan int, 1)
	go func() {
		serverDone <- serve()
		close(serverDone)
	}()

	select {
	case code := <-serverDone:
		expectedShutdown := ctx.Err() != nil
		cancel()
		schedulerErr, ok := <-schedulerDone
		if !ok {
			schedulerErr = nil
		}
		if schedulerErr != nil && !expectedShutdown {
			slog.Error("scheduler stopped", "error", schedulerErr)
			return 1
		}
		return code
	case schedulerErr, ok := <-schedulerDone:
		if ctx.Err() != nil {
			return <-serverDone
		}
		if !ok || schedulerErr == nil {
			slog.Error("scheduler stopped unexpectedly")
		} else {
			slog.Error("scheduler stopped", "error", schedulerErr)
		}
		cancel()
		<-serverDone
		return 1
	}
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
	var secretWriteAuthorizer httpapi.SecretWriteAuthorizer
	if services.Secrets != nil {
		secretWriteAuthorizer, err = httpapi.NewDeploymentSecretWriteAuthorizer(os.Getenv("AGENT_BOARD_SECRET_WRITE_TOKEN"))
		if err != nil {
			_ = services.Close()
			database.Close()
			return nil, nil, fmt.Errorf("configure secret write authorization: %w", err)
		}
	}
	closeApplication := func() {
		if err := services.Close(); err != nil {
			slog.Error("close application services", "error", err)
		}
		database.Close()
	}
	return &applicationHandler{Handler: httpapi.NewRouterWithApplication(services, secretWriteAuthorizer), services: services}, closeApplication, nil
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

func startScheduler(ctx context.Context, handler http.Handler) (<-chan error, error) {
	application, ok := handler.(*applicationHandler)
	if !ok || application.services == nil || application.services.Scheduler == nil {
		return nil, fmt.Errorf("scheduler service is unavailable")
	}
	done := make(chan error, 1)
	go func() {
		done <- application.services.Scheduler.Run(ctx)
		close(done)
	}()
	return done, nil
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
	workspaceRoot := configuredWorkspaceRoot()
	provisioner, err := repository.NewProvisioner(policy, git)
	if err != nil {
		return nil, err
	}
	projectMaterializer, err := workspace.NewProjectMaterializer(database, provisioner, git, workspaceRoot)
	if err != nil {
		return nil, err
	}
	issueMaterializer, err := workspace.NewMaterializer(database, policy, git, workspaceRoot)
	if err != nil {
		return nil, err
	}
	materializer, err := workspace.NewProjectBackedMaterializer(issueMaterializer, projectMaterializer)
	if err != nil {
		return nil, err
	}
	dockerRuntime, dockerErr := dockerruntime.New()
	if dockerErr != nil {
		slog.Warn("Docker runtime unavailable; runner execution remains available", "error", dockerErr)
	}
	secretResolver, err := configuredSecretResolver(database)
	if err != nil {
		if dockerRuntime != nil {
			_ = dockerRuntime.Close()
		}
		return nil, err
	}
	implementations := map[string]runtimepkg.Implementation{}
	if dockerRuntime != nil {
		implementations["docker"] = dockerRuntime
	}
	services, err := app.NewServicesWithRuntimes(database, materializer, implementations, secretResolver)
	if err != nil {
		if dockerRuntime != nil {
			_ = dockerRuntime.Close()
		}
		return nil, err
	}
	if services.ControlPlane != nil && services.ControlPlane.Runners != nil {
		database.SetRunnerCandidates(services.ControlPlane.Runners.Connections.Candidates)
	}
	services.ControlPlane.SetProjectRepositoryProvisioner(provisioner)
	if err := configureExecutionScheduler(services, git); err != nil {
		_ = services.Close()
		return nil, err
	}
	return services, nil
}

func configureExecutionScheduler(services *app.Services, git workspace.Git) error {
	if services == nil || services.ExecutionStore == nil || services.ExecutionContext == nil || services.RuntimeInstances == nil || services.ExecutionSessions == nil || services.Redaction == nil {
		return fmt.Errorf("execution services are incomplete")
	}
	baseBlobs, err := evidence.NewFileBlobStore(configuredEvidenceRoot(), defaultEvidenceBlobLimit)
	if err != nil {
		return err
	}
	blobs, err := evidence.NewRedactingBlobStore(baseBlobs, services.Redaction)
	if err != nil {
		return err
	}
	reviewCandidates, err := evidence.NewReviewCandidateStore(configuredReviewCandidateRoot())
	if err != nil {
		return err
	}
	services.ReviewCandidates = reviewCandidates
	runEvidence, err := app.NewRunEvidenceService(services.ExecutionStore, blobs)
	if err != nil {
		return err
	}
	services.RunEvidence = runEvidence
	hub := evidence.NewHub()
	events, err := evidence.NewRecorder(services.ExecutionStore, hub, func(err error) {
		slog.Error("publish persisted event", "error", err)
	})
	if err != nil {
		return err
	}
	services.EventHub = hub
	services.Events = events
	if services.ControlPlane != nil {
		services.ControlPlane.SetEventRecorder(events)
	}
	if services.Questions != nil {
		services.Questions.SetPersistedEventPublisher(events)
	}
	output, err := evidence.NewOutputRecorder(services.ExecutionStore, blobs, defaultOutputChunkSize)
	if err != nil {
		return err
	}
	candidate, err := evidence.NewCandidateSnapshotterWithReviewCandidates(evidence.NewCandidateCollector(), services.ExecutionStore, blobs, reviewCandidates)
	if err != nil {
		return err
	}
	engines, err := engine.NewRegistry(scripted.New(), opencode.New())
	if err != nil {
		return err
	}
	var runnerConnector runexec.RunnerConnector
	if services.ControlPlane != nil && services.ControlPlane.Runners != nil {
		runnerConnector = runexec.NewRegistryConnector(services.ControlPlane.Runners.Connections)
	}
	processor, err := runexec.NewProcessor(services.ExecutionStore, services.ExecutionContext, services.RuntimeInstances, services.ExecutionSessions, engines, events, output, candidate, git, runnerConnector)
	if err != nil {
		return err
	}
	config := scheduler.DefaultConfig(configuredSchedulerOwnerID())
	config.ReportError = func(err error) { slog.Error("scheduler execution", "error", err) }
	coordinator, err := scheduler.New(services.ExecutionStore, processor, processor, config)
	if err != nil {
		return err
	}
	services.Scheduler = coordinator
	return nil
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

func configuredEvidenceRoot() string {
	if root := strings.TrimSpace(os.Getenv("AGENT_BOARD_EVIDENCE_ROOT")); root != "" {
		return root
	}
	if workspaceRoot := strings.TrimSpace(os.Getenv("AGENT_BOARD_WORKSPACE_ROOT")); workspaceRoot != "" {
		return filepath.Join(filepath.Dir(workspaceRoot), "evidence")
	}
	return defaultEvidenceRoot
}

func configuredReviewCandidateRoot() string {
	return filepath.Join(configuredWorkspaceRoot(), ".review-candidates")
}

func configuredSchedulerOwnerID() string {
	if value := strings.TrimSpace(os.Getenv("AGENT_BOARD_SCHEDULER_OWNER_ID")); value != "" {
		return value
	}
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "agent-board-server"
	}
	return fmt.Sprintf("%s:%d", hostname, os.Getpid())
}

func exitCode(ctx context.Context, address string, handlers ...http.Handler) int {
	if err := run(ctx, address, handlers...); err != nil {
		slog.Error("server stopped", "error", err)
		return 1
	}
	return 0
}

func configuredAddress() string {
	address := normalizeAddress(os.Getenv("AGENT_BOARD_SERVER_ADDR"))
	if address == "" {
		return defaultAddress
	}
	return address
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

func normalizeAddress(address string) string {
	return strings.TrimSpace(address)
}
