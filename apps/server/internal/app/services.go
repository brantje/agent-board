package app

import (
	"errors"
	"fmt"

	evidencepkg "github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

// Services groups the control-plane API with execution-facing services so
// server startup constructs one coherent application boundary.
type Services struct {
	ControlPlane      *Service
	Questions         *QuestionService
	Workspaces        *WorkspaceService
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
	if controlPlaneStore == nil {
		return nil, fmt.Errorf("control-plane store is required")
	}
	controlPlane := New(controlPlaneStore)
	workspaces, err := NewWorkspaceService(controlPlaneStore, materializer)
	if err != nil {
		return nil, err
	}
	services := &Services{ControlPlane: controlPlane, Workspaces: workspaces}
	if store.SupportsQuestionStore(controlPlaneStore) {
		questions, err := NewQuestionService(controlPlaneStore.(store.QuestionStore))
		if err != nil {
			return nil, err
		}
		services.Questions = questions
	}
	return services, nil
}

func NewExecutionServices(controlPlaneStore store.ControlPlaneStore, materializer WorkspaceMaterializer, secretResolvers ...executioncontext.SecretResolver) (*Services, error) {
	registry := redaction.NewRegistry()
	securedStore := evidencepkg.NewRedactingStore(controlPlaneStore, registry)
	services, err := NewServices(securedStore, materializer)
	if err != nil {
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
	var runnerRegistry RunnerRegistry
	if services.ControlPlane.Runners != nil {
		runnerRegistry = services.ControlPlane.Runners.Connections
	}
	transportSessions, err := NewExecutionSessionService(securedStore, runnerRegistry)
	if err != nil {
		return nil, err
	}
	executionSessions, err := NewAuthorizedExecutionSessionService(transportSessions, preparer)
	if err != nil {
		return nil, err
	}
	if services.ControlPlane.Runners != nil {
		services.ControlPlane.Runners.SetSessionTerminator(transportSessions)
		services.ControlPlane.Runners.Connections.SetConnectionReconciler(transportSessions)
	}
	services.ExecutionSessions = executionSessions
	services.ExecutionStore = securedStore
	services.ExecutionContext = resolver
	services.Redaction = registry
	return services, nil
}

func (s *Services) Close() error {
	if s == nil {
		return nil
	}
	var closeErrors []error
	if s.ControlPlane != nil && s.ControlPlane.Runners != nil {
		closeErrors = append(closeErrors, s.ControlPlane.Runners.Connections.Close())
	}
	return errors.Join(closeErrors...)
}
