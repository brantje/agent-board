package evidence

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type capabilityLock struct {
	released bool
}

func (l *capabilityLock) Release() error {
	l.released = true
	return nil
}

type workspaceCapabilityStore struct {
	*captureStore
	bootstrapWorkspaceID string
	executionWorkspaceID string
	executionSessionID   string
	runner               store.Runner
	bootstrapLock        *capabilityLock
	executionLock        *capabilityLock
}

func (s *workspaceCapabilityStore) AcquireWorkspaceBootstrapLock(_ context.Context, workspaceID string) (store.WorkspaceBootstrapLock, error) {
	s.bootstrapWorkspaceID = workspaceID
	return s.bootstrapLock, nil
}

func (s *workspaceCapabilityStore) AcquireWorkspaceExecutionLock(_ context.Context, workspaceID, executionSessionID string) (store.WorkspaceBootstrapLock, error) {
	s.executionWorkspaceID = workspaceID
	s.executionSessionID = executionSessionID
	return s.executionLock, nil
}

func (s *workspaceCapabilityStore) MarkWorkspaceBootstrapPending(context.Context, string, string, string, string, string, string, string, string) (store.Workspace, error) {
	return store.Workspace{}, nil
}

func (s *workspaceCapabilityStore) MarkWorkspaceBootstrapReady(context.Context, string, string, string, string, string, string, string, string) (store.Workspace, error) {
	return store.Workspace{}, nil
}

func (s *workspaceCapabilityStore) MarkWorkspaceBootstrapFailed(context.Context, string, string, string) (store.Workspace, error) {
	return store.Workspace{}, nil
}

func (s *workspaceCapabilityStore) GetRunner(_ context.Context, id string) (store.Runner, error) {
	if id != s.runner.ID {
		return store.Runner{}, store.ErrNotFound
	}
	return s.runner, nil
}

func TestRedactingStoreForwardsRunnerWorkspaceCapabilities(t *testing.T) {
	bootstrapLock := &capabilityLock{}
	executionLock := &capabilityLock{}
	base := &workspaceCapabilityStore{
		captureStore:  &captureStore{},
		runner:        store.Runner{ID: "runner-1", Name: "Runner one"},
		bootstrapLock: bootstrapLock,
		executionLock: executionLock,
	}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())
	ctx := t.Context()

	gotBootstrap, err := wrapped.AcquireWorkspaceBootstrapLock(ctx, "workspace-1")
	if err != nil || gotBootstrap != bootstrapLock || base.bootstrapWorkspaceID != "workspace-1" {
		t.Fatalf("bootstrap lock=%v workspace=%q err=%v", gotBootstrap, base.bootstrapWorkspaceID, err)
	}
	gotExecution, err := wrapped.AcquireWorkspaceExecutionLock(ctx, "workspace-1", "session-1")
	if err != nil || gotExecution != executionLock || base.executionWorkspaceID != "workspace-1" || base.executionSessionID != "session-1" {
		t.Fatalf("execution lock=%v workspace=%q session=%q err=%v", gotExecution, base.executionWorkspaceID, base.executionSessionID, err)
	}
	gotRunner, err := wrapped.GetRunner(ctx, "runner-1")
	if err != nil || gotRunner.ID != "runner-1" {
		t.Fatalf("runner=%+v err=%v", gotRunner, err)
	}
}

func TestRedactingStoreReportsMissingRunnerWorkspaceCapabilities(t *testing.T) {
	wrapped := NewRedactingStore(&captureStore{}, redaction.NewRegistry())
	ctx := t.Context()

	if _, err := wrapped.AcquireWorkspaceBootstrapLock(ctx, "workspace-1"); err == nil {
		t.Fatal("expected missing workspace bootstrap lock capability error")
	}
	if _, err := wrapped.AcquireWorkspaceExecutionLock(ctx, "workspace-1", "session-1"); err == nil {
		t.Fatal("expected missing workspace execution lock capability error")
	}
	if _, err := wrapped.GetRunner(ctx, "runner-1"); err == nil {
		t.Fatal("expected missing runner lookup capability error")
	}
}
