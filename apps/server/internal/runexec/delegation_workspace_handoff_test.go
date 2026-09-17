package runexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationHandoffMark struct {
	projectID      string
	parentRunID    string
	delegationID   string
	delegatedRunID string
}

type localDelegationHandoffStore struct {
	*processTestStore
	runtime *processTestRuntime
	marks   []delegationHandoffMark
	markErr error
}

func (s *localDelegationHandoffStore) MarkDelegationWorkspaceHandoffReady(_ context.Context, projectID, parentRunID, delegationID, delegatedRunID string) error {
	if s.markErr != nil {
		return s.markErr
	}
	if s.runtime != nil && (s.runtime.stopped == 0 || s.runtime.destroyed == 0) {
		return errors.New("handoff marked ready before Runtime cleanup")
	}
	s.marks = append(s.marks, delegationHandoffMark{projectID, parentRunID, delegationID, delegatedRunID})
	return nil
}

type runnerDelegationHandoffStore struct {
	*runnerSyncStore
	marks   []delegationHandoffMark
	markErr error
}

func (s *runnerDelegationHandoffStore) MarkDelegationWorkspaceHandoffReady(_ context.Context, projectID, parentRunID, delegationID, delegatedRunID string) error {
	if s.markErr != nil {
		return s.markErr
	}
	s.marks = append(s.marks, delegationHandoffMark{projectID, parentRunID, delegationID, delegatedRunID})
	return nil
}

type delegationHandoffEngine struct {
	workspace  string
	delegation engine.Delegation
}

func (e delegationHandoffEngine) Name() string { return "test" }

func (e delegationHandoffEngine) Execute(ctx context.Context, request engine.Request) (engine.Result, error) {
	result, err := (processTestEngine{workspace: e.workspace}).Execute(ctx, request)
	if err != nil {
		return engine.Result{}, err
	}
	return result, engine.NewDelegationHandoff(e.delegation)
}

func TestLocalDelegationHandoffFinalizesWorkspaceBeforeYield(t *testing.T) {
	repository := initProcessTestRepository(t)
	safe := processTestSafeContext(repository)
	evidenceStore := &processTestStore{}
	processor, runtimes := newProcessTestProcessor(t, repository, safe, evidenceStore)
	handoffStore := &localDelegationHandoffStore{processTestStore: evidenceStore, runtime: runtimes}
	processor.store = handoffStore
	registry, err := engine.NewRegistry(delegationHandoffEngine{
		workspace:  repository,
		delegation: engine.Delegation{ID: "delegation-1", RunID: "delegated-run-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	processor.engines = registry

	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "PAUSED" || result.FailureReason != nil {
		t.Fatalf("handoff result=%+v", result)
	}
	if len(handoffStore.marks) != 1 {
		t.Fatalf("handoff marks=%+v", handoffStore.marks)
	}
	mark := handoffStore.marks[0]
	if mark.projectID != safe.Project.ID || mark.parentRunID != safe.Run.ID || mark.delegationID != "delegation-1" || mark.delegatedRunID != "delegated-run-1" {
		t.Fatalf("handoff mark=%+v", mark)
	}
	if body, err := os.ReadFile(filepath.Join(repository, "new.txt")); err != nil || string(body) != "finalized-value\n" {
		t.Fatalf("finalized parent work=%q err=%v", body, err)
	}
	assertProcessTestRepositoryFinalized(t, repository, safe)
	if !hasProcessTestEvent(evidenceStore.events, "run.paused") || hasProcessTestEvent(evidenceStore.events, "run.ready_for_review") || hasProcessTestEvent(evidenceStore.events, "run.failed") {
		t.Fatalf("handoff events=%+v", evidenceStore.events)
	}
}

func TestRunnerDelegationHandoffSyncsWorkspaceBeforeYield(t *testing.T) {
	repository := initProcessTestRepository(t)
	safe := processTestSafeContext(repository)
	baseStore := &runnerSyncStore{}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, repository)}
	processor := newRunnerSyncProcessor(t, repository, safe, baseStore, client)
	handoffStore := &runnerDelegationHandoffStore{runnerSyncStore: baseStore}
	processor.store = handoffStore
	registry, err := engine.NewRegistry(delegationHandoffEngine{
		workspace:  repository,
		delegation: engine.Delegation{ID: "delegation-1", RunID: "delegated-run-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	processor.engines = registry
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

	result, err := processor.runEngineOnRunner(t.Context(), run, safe, "runner-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "PAUSED" || len(handoffStore.marks) != 1 {
		t.Fatalf("result=%+v marks=%+v", result, handoffStore.marks)
	}
	if len(client.directions) != 1 || client.directions[0] != "from_runner" || !client.confirmed {
		t.Fatalf("workspace hand-back directions=%v confirmed=%v", client.directions, client.confirmed)
	}
	if body, err := os.ReadFile(filepath.Join(repository, "returned.txt")); err != nil || string(body) != "from runner\n" {
		t.Fatalf("returned delegate-boundary work=%q err=%v", body, err)
	}
	if !hasProcessTestEvent(baseStore.events, "run.paused") || hasProcessTestEvent(baseStore.events, "run.ready_for_review") || hasProcessTestEvent(baseStore.events, "run.failed") {
		t.Fatalf("handoff events=%+v", baseStore.events)
	}
}

func TestDelegationHandoffFailureDoesNotReleaseChild(t *testing.T) {
	repository := initProcessTestRepository(t)
	safe := processTestSafeContext(repository)
	baseStore := &runnerSyncStore{}
	client := &failingSyncClient{}
	processor := newRunnerSyncProcessor(t, repository, safe, baseStore, client)
	handoffStore := &runnerDelegationHandoffStore{runnerSyncStore: baseStore}
	processor.store = handoffStore
	registry, err := engine.NewRegistry(delegationHandoffEngine{
		workspace:  repository,
		delegation: engine.Delegation{ID: "delegation-1", RunID: "delegated-run-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	processor.engines = registry
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

	result, err := processor.runEngineOnRunner(t.Context(), run, safe, "runner-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "FAILED" || result.FailureReason == nil {
		t.Fatalf("failed handoff result=%+v", result)
	}
	if len(handoffStore.marks) != 0 {
		t.Fatalf("failed handoff marked child ready: %+v", handoffStore.marks)
	}
	if !hasProcessTestEvent(baseStore.events, "workspace.transfer.failed") || !hasProcessTestEvent(baseStore.events, "run.failed") || hasProcessTestEvent(baseStore.events, "run.paused") {
		t.Fatalf("failed handoff events=%+v", baseStore.events)
	}
}

func TestDelegationHandoffCancellationSyncsButDoesNotReleaseChild(t *testing.T) {
	repository := initProcessTestRepository(t)
	safe := processTestSafeContext(repository)
	baseStore := &runnerSyncStore{}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, repository)}
	processor := newRunnerSyncProcessor(t, repository, safe, baseStore, client)
	handoffStore := &runnerDelegationHandoffStore{runnerSyncStore: baseStore}
	processor.store = handoffStore
	registry, err := engine.NewRegistry(delegationHandoffEngine{
		workspace:  repository,
		delegation: engine.Delegation{ID: "delegation-1", RunID: "delegated-run-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	processor.engines = registry
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := processor.runEngineOnRunner(ctx, run, safe, "runner-1", "session-1")
	if !errors.Is(err, context.Canceled) || result.RunStatus != "" {
		t.Fatalf("cancel result=%+v err=%v", result, err)
	}
	if len(handoffStore.marks) != 0 {
		t.Fatalf("cancelled parent released child: %+v", handoffStore.marks)
	}
	if len(client.directions) != 1 || client.directions[0] != "from_runner" || !client.confirmed {
		t.Fatalf("cancelled parent did not preserve hand-back: directions=%v confirmed=%v", client.directions, client.confirmed)
	}
}
