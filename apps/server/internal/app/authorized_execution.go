package app

import (
	"context"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type ExecutionPreparer interface {
	Prepare(context.Context, string, string, executioncontext.SecretRequest) (executioncontext.Prepared, error)
}

type AuthorizedExecutionRequest struct {
	Command               []string
	CWD                   string
	Env                   map[string]string
	ProviderCredentialEnv string
	RuntimeSecretRefs     map[string]string
}

// AuthorizedExecutionSessionService is the server-owned launch boundary. It
// resolves immutable execution context and authorized secret references before
// delegating the already-scoped request to the runner transport service.
type AuthorizedExecutionSessionService struct {
	sessions *ExecutionSessionService
	preparer ExecutionPreparer
}

func NewAuthorizedExecutionSessionService(sessions *ExecutionSessionService, preparer ExecutionPreparer) (*AuthorizedExecutionSessionService, error) {
	if sessions == nil || preparer == nil {
		return nil, NewError("execution_preparation_unavailable", "Execution preparation is unavailable", store.ErrInvalidArgument)
	}
	return &AuthorizedExecutionSessionService{sessions: sessions, preparer: preparer}, nil
}

func (s *AuthorizedExecutionSessionService) Start(ctx context.Context, projectID, runID, runtimeInstanceID string, request AuthorizedExecutionRequest) (*ExecutionProcess, error) {
	prepared, err := s.preparer.Prepare(ctx, projectID, runID, executioncontext.SecretRequest{
		ProviderCredentialEnv: request.ProviderCredentialEnv,
		RuntimeSecretRefs:     cloneMap(request.RuntimeSecretRefs),
	})
	if err != nil {
		return nil, translateExecutionPreparationError(err)
	}
	instance, err := s.sessions.store.GetRuntimeInstance(ctx, projectID, runtimeInstanceID)
	if err != nil {
		return nil, translateStoreError(err, "runtime_instance")
	}
	if instance.RuntimeID != prepared.RuntimeID {
		return nil, NewError("runtime_configuration_mismatch", "Runtime Instance does not match the resolved execution Runtime", store.ErrInvalidArgument)
	}
	process, err := s.sessions.Start(ctx, projectID, runID, runtimeInstanceID, ExecutionRequest{
		Command: append([]string(nil), request.Command...),
		CWD:     request.CWD,
		Env:     cloneMap(request.Env),
		Secrets: cloneMap(prepared.Secrets),
	})
	if err != nil {
		return nil, redaction.WrapError(err, prepared.RedactionValues)
	}
	return process, nil
}

func (s *AuthorizedExecutionSessionService) ReconcileAll(ctx context.Context) error {
	return s.sessions.ReconcileAll(ctx)
}

func (s *AuthorizedExecutionSessionService) ReconcileAllWithReporter(ctx context.Context, report func(error)) error {
	return s.sessions.ReconcileAllWithReporter(ctx, report)
}

func (s *AuthorizedExecutionSessionService) Cancel(ctx context.Context, projectID, sessionID string, grace time.Duration) error {
	return s.sessions.Cancel(ctx, projectID, sessionID, grace)
}

func translateExecutionPreparationError(err error) error {
	if executionErr, ok := executioncontext.AsError(err); ok {
		return NewError(executionErr.Code, executionErr.Message, executionErr.Cause)
	}
	return err
}
