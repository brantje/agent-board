package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type lifecycleFenceRuntimeStore struct {
	*runtimeServiceStore
	mutateBeforeWrite bool
	expectedStatus    string
	generationStatus  string
}

func (s *lifecycleFenceRuntimeStore) UpdateRuntimeInstanceRunnerStatusIfStatus(_ context.Context, projectID, instanceID, runnerStatus, expectedStatus string) (store.RuntimeInstance, error) {
	s.expectedStatus = expectedStatus
	if s.mutateBeforeWrite {
		s.instance.Status = "STOPPING"
	}
	if s.instance.ID != instanceID || s.instance.ProjectID != projectID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	if s.instance.Status != expectedStatus {
		return store.RuntimeInstance{}, store.ErrConflict
	}
	s.instance.RunnerStatus = runnerStatus
	return s.instance, nil
}

func (s *lifecycleFenceRuntimeStore) ClaimRuntimeInstanceRunnerGeneration(context.Context, string, string) (int64, error) {
	return 1, nil
}

func (s *lifecycleFenceRuntimeStore) UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(_ context.Context, projectID, instanceID, runnerStatus string, generation int64, expectedStatus string) (store.RuntimeInstance, error) {
	s.generationStatus = expectedStatus
	if generation != 1 {
		return store.RuntimeInstance{}, store.ErrConflict
	}
	return s.UpdateRuntimeInstanceRunnerStatusIfStatus(context.Background(), projectID, instanceID, runnerStatus, expectedStatus)
}

func TestRunnerStatusWritesRejectConcurrentLifecycleMovement(t *testing.T) {
	service, runtimeStore, _, workspace := runtimeServiceFixture(t)
	runtimeStore.instance = store.RuntimeInstance{
		ID:           "instance-1",
		ProjectID:    workspace.ProjectID,
		WorkspaceID:  workspace.ID,
		RuntimeID:    runtimeStore.runtime.ID,
		Status:       "RUNNING",
		RunnerStatus: "READY",
	}
	fenced := &lifecycleFenceRuntimeStore{runtimeServiceStore: runtimeStore, mutateBeforeWrite: true}
	service.store = fenced

	if err := service.SetRunnerStatus(context.Background(), workspace.ProjectID, runtimeStore.instance.ID, "BUSY"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("SetRunnerStatus() error=%v", err)
	}
	if fenced.expectedStatus != "RUNNING" || runtimeStore.instance.RunnerStatus != "READY" {
		t.Fatalf("expectedStatus=%q instance=%+v", fenced.expectedStatus, runtimeStore.instance)
	}

	runtimeStore.instance.Status = "RUNNING"
	runtimeStore.instance.RunnerStatus = "READY"
	if err := service.SetRunnerStatusGeneration(context.Background(), workspace.ProjectID, runtimeStore.instance.ID, "UNAVAILABLE", 1); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("SetRunnerStatusGeneration() error=%v", err)
	}
	if fenced.generationStatus != "RUNNING" || runtimeStore.instance.RunnerStatus != "READY" {
		t.Fatalf("generationStatus=%q instance=%+v", fenced.generationStatus, runtimeStore.instance)
	}
}
