package runexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type sequenceProcessResolver struct {
	values []executioncontext.Resolved
	index  int
}

func (r *sequenceProcessResolver) Resolve(context.Context, string, string) (executioncontext.Resolved, error) {
	if len(r.values) == 0 {
		return executioncontext.Resolved{}, errors.New("no resolved context")
	}
	index := r.index
	if index >= len(r.values) {
		index = len(r.values) - 1
	}
	r.index++
	return r.values[index], nil
}

type failingProcessLifecycle struct{ err error }

func (l failingProcessLifecycle) Running(context.Context) (store.Run, error) {
	return store.Run{}, l.err
}

func newFailureCoverageProcessor(t *testing.T, workspace string, resolver ContextResolver, adapter engine.Engine) (*Processor, *processTestStore, *processTestRuntime) {
	t.Helper()
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
	registry, err := engine.NewRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	runtimes := &processTestRuntime{}
	processor, err := NewProcessor(evidenceStore, resolver, runtimes, processTestSessions{}, registry, recorder, output, candidate)
	if err != nil {
		t.Fatal(err)
	}
	return processor, evidenceStore, runtimes
}

func TestProcessorProcessCoversFailureBoundaries(t *testing.T) {
	workspace := initProcessTestRepository(t)
	safe := processTestSafeContext(workspace)
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	claim := &store.SchedulerAdmission{Run: run}
	lifecycle := processTestLifecycle{run: run}

	t.Run("engine failure records terminal evidence and cleans runtime", func(t *testing.T) {
		processor, evidenceStore, runtimes := newFailureCoverageProcessor(
			t,
			workspace,
			processTestResolver{resolved: executioncontext.Resolved{Safe: safe}},
			processTestEngine{workspace: workspace, fail: errors.New("engine failed")},
		)
		result, err := processor.Process(t.Context(), claim, lifecycle)
		if err != nil {
			t.Fatal(err)
		}
		if result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "engine failed") {
			t.Fatalf("result=%+v", result)
		}
		if runtimes.stopped != 1 || runtimes.destroyed != 1 {
			t.Fatalf("runtime cleanup stop/destroy=%d/%d", runtimes.stopped, runtimes.destroyed)
		}
		if !hasProcessTestEvent(evidenceStore.events, "run.failed") {
			t.Fatalf("run.failed event missing: %+v", evidenceStore.events)
		}
	})

	t.Run("unknown engine fails after safe runtime cleanup", func(t *testing.T) {
		missing := safe
		missing.Executor.Engine = "missing"
		processor, _, runtimes := newFailureCoverageProcessor(
			t,
			workspace,
			processTestResolver{resolved: executioncontext.Resolved{Safe: missing}},
			processTestEngine{workspace: workspace},
		)
		result, err := processor.Process(t.Context(), claim, lifecycle)
		if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "not registered") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if runtimes.destroyed != 1 {
			t.Fatalf("destroyed=%d, want 1", runtimes.destroyed)
		}
	})

	t.Run("binding drift is rejected before engine execution", func(t *testing.T) {
		drifted := safe
		drifted.Workspace.ID = "different-workspace"
		resolver := &sequenceProcessResolver{values: []executioncontext.Resolved{{Safe: safe}, {Safe: drifted}}}
		processor, _, runtimes := newFailureCoverageProcessor(t, workspace, resolver, processTestEngine{workspace: workspace})
		result, err := processor.Process(t.Context(), claim, lifecycle)
		if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "bindings changed") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if runtimes.destroyed != 1 {
			t.Fatalf("destroyed=%d, want 1", runtimes.destroyed)
		}
	})

	t.Run("lifecycle failure is returned without execution", func(t *testing.T) {
		processor, _, runtimes := newFailureCoverageProcessor(
			t,
			workspace,
			processTestResolver{resolved: executioncontext.Resolved{Safe: safe}},
			processTestEngine{workspace: workspace},
		)
		_, err := processor.Process(t.Context(), claim, failingProcessLifecycle{err: errors.New("cannot mark running")})
		if err == nil || !strings.Contains(err.Error(), "cannot mark running") {
			t.Fatalf("err=%v", err)
		}
		if runtimes.created != 0 {
			t.Fatalf("runtime created=%d after lifecycle failure", runtimes.created)
		}
	})

	t.Run("nil admission is rejected", func(t *testing.T) {
		processor, _, _ := newFailureCoverageProcessor(
			t,
			workspace,
			processTestResolver{resolved: executioncontext.Resolved{Safe: safe}},
			processTestEngine{workspace: workspace},
		)
		if _, err := processor.Process(t.Context(), nil, lifecycle); err == nil {
			t.Fatal("expected nil scheduler admission error")
		}
	})
}

var _ scheduler.Lifecycle = failingProcessLifecycle{}
