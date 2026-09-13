package app

import (
	"errors"
	"fmt"

	evidencepkg "github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

// Services groups the control-plane API with execution-facing services so
// server startup constructs one coherent application boundary.
type Services struct {
	ControlPlane      *Service
	Auth              *AuthService
	ProjectAccess     *ProjectAccessService
	Questions         *QuestionService
	Workspaces        *WorkspaceService
	RuntimeInstances  *RuntimeInstanceService
	RunnerConnections *runner.Manager
	ExecutionSessions *AuthorizedExecutionSessionService
	RunEvidence       *RunEvidenceService
	ExecutionStore    store.ControlPlaneStore
	ExecutionContext  *executioncontext.Resolver
	Scheduler         *scheduler.Coordinator
	Redaction         *redaction.Registry
	Secrets           SecretWriter
	EventHub          *evidencepkg.Hub
	Events            *evidencepkg.Recorder
}

func NewServices(controlPlaneStore store.ControlPlaneStore, materializer WorkspaceMaterializer) (*Services, error) {
	return newServices(controlPlaneStore, materializer, AuthServiceConfig{})
}

func newServices(controlPlaneStore store.ControlPlaneStore, materializer WorkspaceMaterializer, authConfig AuthServiceConfig) (*Services, error) {
	if controlPlaneStore == nil {
		return nil, fmt.Errorf("control-plane store is required")
	}
	controlPlane := New(controlPlaneStore)
	workspaces, err := NewWorkspaceService(controlPlaneStore, materializer)
	if err != nil {
		return nil, err
	}
	services := &Services{ControlPlane: controlPlane, Workspaces: workspaces}
	if err := configureAuth(services, controlPlaneStore, authConfig); err != nil {
		return nil, err
	}
	if err := configureProjectAccess(services, controlPlaneStore); err != nil {
		return nil, err
	}
	if store.SupportsQuestionStore(controlPlaneStore) {
		questions, err := NewQuestionService(controlPlaneStore.(store.QuestionStore))
		if err != nil {
			return nil, err
		}
		services.Questions = questions
	}
	return services, nil
}

func NewServicesWithRuntimes(controlPlaneStore store.ControlPlaneStore, materializer WorkspaceMaterializer, implementations map[string]runtimepkg.Implementation, secretResolvers ...executioncontext.SecretResolver) (*Services, error) {
	return newServicesWithRuntimes(controlPlaneStore, materializer, implementations, AuthServiceConfig{}, secretResolvers...)
}

// NewServicesWithRuntimesAuthConfig is the production-capable constructor when
// authentication needs deployment-stable signing configuration. Existing tests
// and non-auth stores may continue using NewServicesWithRuntimes.
func NewServicesWithRuntimesAuthConfig(controlPlaneStore store.ControlPlaneStore, materializer WorkspaceMaterializer, implementations map[string]runtimepkg.Implementation, authConfig AuthServiceConfig, secretResolvers ...executioncontext.SecretResolver) (*Services, error) {
	return newServicesWithRuntimes(controlPlaneStore, materializer, implementations, authConfig, secretResolvers...)
}

func newServicesWithRuntimes(controlPlaneStore store.ControlPlaneStore, materializer WorkspaceMaterializer, implementations map[string]runtimepkg.Implementation, authConfig AuthServiceConfig, secretResolvers ...executioncontext.SecretResolver) (*Services, error) {
	registry := redaction.NewRegistry()
	securedStore := evidencepkg.NewRedactingStore(controlPlaneStore, registry)
	services, err := newServices(securedStore, materializer, authConfig)
	if err != nil {
		return nil, err
	}
	// Authentication and Project access persistence are control-plane security
	// state, not execution evidence. Bind both to the authoritative base store
	// instead of teaching the evidence redaction decorator unrelated methods.
	if err := configureAuth(services, controlPlaneStore, authConfig); err != nil {
		return nil, err
	}
	if err := configureProjectAccess(services, controlPlaneStore); err != nil {
		return nil, err
	}
	if runners, ok := controlPlaneStore.(store.RunnerStore); ok {
		services.ControlPlane.Runners = NewRunnerService(runners)
	}
	resolver, err := executioncontext.NewResolver(securedStore)
	if err != nil {
		return nil, err
	}
	var secretResolver executioncontext.SecretResolver
	if len(secretResolvers) > 0 {
		secretResolver = secretResolvers[0]
		if writer, ok := secretResolver.(SecretWriter); ok {
			services.Secrets = writer
		}
	}
	preparer, err := executioncontext.NewPreparer(resolver, secretResolver, securedStore, registry)
	if err != nil {
		return nil, err
	}
	runtimeInstances, err := NewRuntimeInstanceService(securedStore, services.Workspaces, implementations)
	if err != nil {
		return nil, err
	}
	runnerConnections, err := runner.NewManager(runtimeInstances, runtimeInstances)
	if err != nil {
		_ = runtimeInstances.Close()
		return nil, err
	}
	var runnerRegistry RunnerRegistry
	if services.ControlPlane.Runners != nil {
		runnerRegistry = services.ControlPlane.Runners.Connections
	}
	transportSessions, err := NewExecutionSessionService(securedStore, runnerConnections, runnerRegistry)
	if err != nil {
		_ = runnerConnections.Close()
		_ = runtimeInstances.Close()
		return nil, err
	}
	executionSessions, err := NewAuthorizedExecutionSessionService(transportSessions, preparer)
	if err != nil {
		_ = runnerConnections.Close()
		_ = runtimeInstances.Close()
		return nil, err
	}
	if services.ControlPlane.Runners != nil {
		services.ControlPlane.Runners.SetSessionTerminator(transportSessions)
		services.ControlPlane.Runners.Connections.SetConnectionReconciler(transportSessions)
	}
	services.RuntimeInstances = runtimeInstances
	services.RunnerConnections = runnerConnections
	services.ExecutionSessions = executionSessions
	services.ExecutionStore = securedStore
	services.ExecutionContext = resolver
	services.Redaction = registry
	return services, nil
}

func configureAuth(services *Services, candidate any, config AuthServiceConfig) error {
	authStore, ok := candidate.(store.AuthStore)
	if !ok {
		return nil
	}
	auth, err := NewAuthService(authStore, config)
	if err != nil {
		return fmt.Errorf("configure authentication: %w", err)
	}
	services.Auth = auth
	return nil
}

func configureProjectAccess(services *Services, candidate any) error {
	accessStore, ok := candidate.(store.ProjectAccessStore)
	if !ok {
		return nil
	}
	access, err := NewProjectAccessService(services.ControlPlane, accessStore)
	if err != nil {
		return fmt.Errorf("configure project access: %w", err)
	}
	services.ProjectAccess = access
	return nil
}

func (s *Services) Close() error {
	if s == nil {
		return nil
	}
	var closeErrors []error
	if s.ControlPlane != nil && s.ControlPlane.Runners != nil {
		closeErrors = append(closeErrors, s.ControlPlane.Runners.Connections.Close())
	}
	// Close transport first so no runner operation can race Runtime teardown.
	if s.RunnerConnections != nil {
		closeErrors = append(closeErrors, s.RunnerConnections.Close())
	}
	if s.RuntimeInstances != nil {
		closeErrors = append(closeErrors, s.RuntimeInstances.Close())
	}
	return errors.Join(closeErrors...)
}
