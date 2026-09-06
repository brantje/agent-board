package app

import (
	"context"
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runtimeRunnerCapabilityStore struct {
	RuntimeInstanceStore
	instance   store.RuntimeInstance
	generation int64
}

func (s *runtimeRunnerCapabilityStore) GetRuntimeInstance(_ context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	if projectID != s.instance.ProjectID || instanceID != s.instance.ID {
		return store.RuntimeInstance{}, store.ErrNotFound
	}
	return s.instance, nil
}
func (s *runtimeRunnerCapabilityStore) UpdateRuntimeInstanceRunnerStatusIfStatus(_ context.Context, _, _ string, status, expectedStatus string) (store.RuntimeInstance, error) {
	if s.instance.Status != expectedStatus {
		return store.RuntimeInstance{}, store.ErrConflict
	}
	s.instance.RunnerStatus = status
	return s.instance, nil
}
func (s *runtimeRunnerCapabilityStore) ClaimRuntimeInstanceRunnerGeneration(context.Context, string, string) (int64, error) {
	s.generation++
	return s.generation, nil
}
func (s *runtimeRunnerCapabilityStore) UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(_ context.Context, _, _ string, status string, generation int64, expectedStatus string) (store.RuntimeInstance, error) {
	if generation != s.generation || s.instance.Status != expectedStatus {
		return store.RuntimeInstance{}, store.ErrConflict
	}
	s.instance.RunnerStatus = status
	return s.instance, nil
}

func TestRuntimeInstanceServiceUsesRunnerFencingCapabilities(t *testing.T) {
	base := &runtimeRunnerCapabilityStore{instance: store.RuntimeInstance{
		ID: "runtime-instance", ProjectID: "project", WorkspaceID: "workspace", RuntimeID: "runtime", Status: string(runtimepkg.StateRunning),
	}}
	service := &RuntimeInstanceService{store: base}

	generation, err := service.ClaimRunnerConnection(t.Context(), "project", "runtime-instance")
	if err != nil || generation != 1 {
		t.Fatalf("claim generation=%d err=%v", generation, err)
	}
	if err := service.SetRunnerStatusGeneration(t.Context(), "project", "runtime-instance", "READY", generation); err != nil {
		t.Fatalf("set generation status: %v", err)
	}
	if base.instance.RunnerStatus != "READY" {
		t.Fatalf("runner status=%q", base.instance.RunnerStatus)
	}
	if err := service.SetRunnerStatus(t.Context(), "project", "runtime-instance", "BUSY"); err != nil {
		t.Fatalf("set lifecycle-fenced status: %v", err)
	}
	if base.instance.RunnerStatus != "BUSY" {
		t.Fatalf("runner status=%q", base.instance.RunnerStatus)
	}
}

func TestRuntimeInstanceServiceRejectsRunnerClaimWhenNotRunning(t *testing.T) {
	base := &runtimeRunnerCapabilityStore{instance: store.RuntimeInstance{
		ID: "runtime-instance", ProjectID: "project", WorkspaceID: "workspace", RuntimeID: "runtime", Status: string(runtimepkg.StateStopped),
	}}
	service := &RuntimeInstanceService{store: base}
	_, err := service.ClaimRunnerConnection(t.Context(), "project", "runtime-instance")
	if err == nil {
		t.Fatal("expected non-running Runtime Instance to reject runner claim")
	}
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "runtime_instance_not_running" {
		t.Fatalf("error=%v", err)
	}
}

type runtimeRunnerBasicStore struct{ RuntimeInstanceStore }

func TestRuntimeInstanceServiceReportsMissingRunnerFencingCapabilities(t *testing.T) {
	service := &RuntimeInstanceService{store: &runtimeRunnerBasicStore{}}
	if _, err := service.ClaimRunnerConnection(t.Context(), "project", "runtime-instance"); err == nil {
		t.Fatal("expected missing generation capability error")
	}
	if err := service.SetRunnerStatusGeneration(t.Context(), "project", "runtime-instance", "READY", 1); err == nil {
		t.Fatal("expected missing generation status capability error")
	}
	if err := service.SetRunnerStatus(t.Context(), "project", "runtime-instance", "READY"); err == nil {
		t.Fatal("expected missing lifecycle-fenced status capability error")
	}
}
