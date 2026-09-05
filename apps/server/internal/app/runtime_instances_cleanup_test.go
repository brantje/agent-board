package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type createStatePersistenceFailureStore struct {
	*runtimeServiceStore
	err error
}

func (s *createStatePersistenceFailureStore) UpdateRuntimeInstanceState(context.Context, string, string, string, *string, string, json.RawMessage) (store.RuntimeInstance, error) {
	return store.RuntimeInstance{}, s.err
}

func TestRuntimeInstanceServiceCreateDestroysRuntimeWhenIdentityPersistenceFails(t *testing.T) {
	service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
	persistErr := errors.New("persist runtime identity")
	service.store = &createStatePersistenceFailureStore{runtimeServiceStore: runtimeStore, err: persistErr}

	_, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
	if !errors.Is(err, persistErr) {
		t.Fatalf("Create() error=%v, want persistence error", err)
	}
	if implementation.destroyCalls != 1 {
		t.Fatalf("Destroy() calls=%d, want 1", implementation.destroyCalls)
	}
}

func TestRuntimeInstanceServiceCreateReportsCleanupFailureWithoutLosingPersistenceError(t *testing.T) {
	service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
	persistErr := errors.New("persist runtime identity")
	destroyErr := errors.New("destroy runtime")
	implementation.destroyErr = destroyErr
	service.store = &createStatePersistenceFailureStore{runtimeServiceStore: runtimeStore, err: persistErr}

	_, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
	if !errors.Is(err, persistErr) || !errors.Is(err, destroyErr) {
		t.Fatalf("Create() error=%v, want persistence and cleanup errors", err)
	}
	if implementation.destroyCalls != 1 {
		t.Fatalf("Destroy() calls=%d, want 1", implementation.destroyCalls)
	}
}

var _ RuntimeInstanceStore = (*createStatePersistenceFailureStore)(nil)
var _ runtimepkg.Implementation = (*fakeRuntimeImplementation)(nil)
