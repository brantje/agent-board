package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestReconcileAllWithReporterSeparatesInstanceAndStoreFailures(t *testing.T) {
	service, base, implementation, workspace := runtimeServiceFixture(t)
	externalID := "container-1"
	base.instance = store.RuntimeInstance{
		ID:                 "instance-1",
		ProjectID:          workspace.ProjectID,
		WorkspaceID:        workspace.ID,
		RuntimeID:          base.runtime.ID,
		Status:             string(runtimepkg.StateRunning),
		ExternalID:         &externalID,
		SafeHandleMetadata: json.RawMessage(`{"safe":true}`),
	}
	rs := &reconcileStore{runtimeServiceStore: base, workspace: workspace}
	service.store = rs

	inspectErr := errors.New("inspect failed")
	service.implementations["docker"] = &inspectErrorRuntime{fakeRuntimeImplementation: implementation, err: inspectErr}
	var reported []error
	if err := service.ReconcileAllWithReporter(context.Background(), func(err error) {
		reported = append(reported, err)
	}); err != nil {
		t.Fatalf("ReconcileAllWithReporter() fatal error=%v", err)
	}
	if len(reported) != 1 || !errors.Is(reported[0], inspectErr) {
		t.Fatalf("reported=%v", reported)
	}

	reported = nil
	listErr := errors.New("list failed")
	service.store = &listInstancesErrorStore{reconcileStore: rs, err: listErr}
	if err := service.ReconcileAllWithReporter(context.Background(), func(err error) {
		reported = append(reported, err)
	}); !errors.Is(err, listErr) {
		t.Fatalf("ReconcileAllWithReporter() store error=%v", err)
	}
	if len(reported) != 0 {
		t.Fatalf("store error was incorrectly reported as per-instance: %v", reported)
	}
}
