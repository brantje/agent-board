package runexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type startFailureRuntime struct {
	stopProjectID    string
	stopInstanceID   string
	destroyProjectID string
	destroyInstanceID string
}

func (r *startFailureRuntime) Create(_ context.Context, projectID, _, runtimeID string) (store.RuntimeInstance, error) {
	return store.RuntimeInstance{
		ID:        "runtime-instance",
		ProjectID: projectID,
		RuntimeID: runtimeID,
		Status:    string(runtimepkg.StateProvisioning),
	}, nil
}

func (*startFailureRuntime) Start(context.Context, string, string) (store.RuntimeInstance, error) {
	return store.RuntimeInstance{}, errors.New("runtime start failed")
}

func (r *startFailureRuntime) Stop(_ context.Context, projectID, instanceID string, _ runtimepkg.StopReason) (store.RuntimeInstance, error) {
	r.stopProjectID = projectID
	r.stopInstanceID = instanceID
	return store.RuntimeInstance{ID: instanceID, ProjectID: projectID, Status: string(runtimepkg.StateStopped)}, nil
}

func (r *startFailureRuntime) Destroy(_ context.Context, projectID, instanceID string) (store.RuntimeInstance, error) {
	r.destroyProjectID = projectID
	r.destroyInstanceID = instanceID
	return store.RuntimeInstance{ID: instanceID, ProjectID: projectID, Status: string(runtimepkg.StateDestroyed)}, nil
}

func TestProcessorPreservesCreatedRuntimeIdentityWhenStartFails(t *testing.T) {
	workspace := initProcessTestRepository(t)
	safe := processTestSafeContext(workspace)
	evidenceStore := &processTestStore{}
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(evidenceStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(evidenceStore, blobs, 64)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := evidence.NewCandidateSnapshotter(evidence.NewCandidateCollector(), evidenceStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := engine.NewRegistry(processTestEngine{workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	runtimes := &startFailureRuntime{}
	processor, err := NewProcessor(
		evidenceStore,
		processTestResolver{resolved: executioncontext.Resolved{Safe: safe}},
		runtimes,
		processTestSessions{},
		registry,
		recorder,
		output,
		candidate,
	)
	if err != nil {
		t.Fatal(err)
	}
	run := store.Run{
		ID:          safe.Run.ID,
		ProjectID:   safe.Project.ID,
		IssueID:     safe.Issue.ID,
		WorkspaceID: safe.Workspace.ID,
		Status:      "STARTING",
	}

	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "runtime start failed") {
		t.Fatalf("result=%+v", result)
	}
	if runtimes.stopProjectID != safe.Project.ID || runtimes.stopInstanceID != "runtime-instance" {
		t.Fatalf("stop scope=%q/%q, want %q/runtime-instance", runtimes.stopProjectID, runtimes.stopInstanceID, safe.Project.ID)
	}
	if runtimes.destroyProjectID != safe.Project.ID || runtimes.destroyInstanceID != "runtime-instance" {
		t.Fatalf("destroy scope=%q/%q, want %q/runtime-instance", runtimes.destroyProjectID, runtimes.destroyInstanceID, safe.Project.ID)
	}
}
