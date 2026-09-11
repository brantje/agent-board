package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runnerSnapshotFailureStore struct {
	*runnerSyncStore
	err error
}

func (s *runnerSnapshotFailureStore) CreateArtifact(context.Context, store.Artifact) (store.Artifact, error) {
	return store.Artifact{}, s.err
}

type runnerWorkspaceLockFailureStore struct {
	*runnerSyncStore
	err error
}

func (s *runnerWorkspaceLockFailureStore) AcquireWorkspaceExecutionLock(context.Context, string, string) (store.WorkspaceBootstrapLock, error) {
	return nil, s.err
}

type runnerOutboundFailureClient struct {
	*successfulSyncClient
	err error
}

func (c *runnerOutboundFailureClient) SendTransfer(_ context.Context, _, _, direction string, _ []byte, _ runner.TransferProgressFunc) error {
	c.directions = append(c.directions, direction)
	return c.err
}

type failingRunnerSyncConnector struct{ err error }

func (c failingRunnerSyncConnector) Connect(context.Context, string, string) (runnerClient, error) {
	return nil, c.err
}

func TestRunEngineOnRunnerTerminalOutcomes(t *testing.T) {
	t.Run("unknown engine fails before runner sync", func(t *testing.T) {
		repo := initProcessTestRepository(t)
		safe := processTestSafeContext(repo)
		safe.Agent.Engine = "missing"
		storeFake := &runnerSyncStore{}
		client := &successfulSyncClient{payload: runnerTransferPayload(t, repo)}
		processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)
		run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

		result, err := processor.runEngineOnRunner(t.Context(), run, safe, "runner-1", "session-1")
		if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if len(client.directions) != 0 {
			t.Fatalf("unknown engine unexpectedly transferred workspace: %v", client.directions)
		}
	})

	t.Run("workspace lock failure blocks transfer and execution", func(t *testing.T) {
		repo := initProcessTestRepository(t)
		safe := processTestSafeContext(repo)
		safe.Runtime = executioncontext.RuntimeContext{}
		storeFake := &runnerSyncStore{}
		client := &successfulSyncClient{}
		processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)
		processor.store = &runnerWorkspaceLockFailureStore{runnerSyncStore: storeFake, err: errors.New("workspace already has an execution writer")}
		run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}

		result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
		if err != nil {
			t.Fatal(err)
		}
		if result.RunStatus != "FAILED" {
			t.Fatalf("workspace lock failure result=%+v", result)
		}
		if len(client.directions) != 0 {
			t.Fatalf("workspace lock failure still transferred data: %v", client.directions)
		}
		if !hasProcessTestEvent(storeFake.events, "workspace.transfer.failed") || hasProcessTestEvent(storeFake.events, "agent.message") || hasProcessTestEvent(storeFake.events, "run.ready_for_review") {
			t.Fatalf("workspace lock failure events=%+v", storeFake.events)
		}
	})

	t.Run("runner connection failure blocks workspace transfer and execution", func(t *testing.T) {
		repo := initProcessTestRepository(t)
		safe := processTestSafeContext(repo)
		safe.Runtime = executioncontext.RuntimeContext{}
		storeFake := &runnerSyncStore{}
		processor := newRunnerSyncProcessor(t, repo, safe, storeFake, &successfulSyncClient{})
		processor.runners = failingRunnerSyncConnector{err: errors.New("runner disconnected before transfer")}
		run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}

		result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
		if err != nil {
			t.Fatal(err)
		}
		if result.RunStatus != "FAILED" || result.FailureReason == nil {
			t.Fatalf("runner disconnect result=%+v", result)
		}
		if !hasProcessTestEvent(storeFake.events, "workspace.transfer.failed") || hasProcessTestEvent(storeFake.events, "agent.message") || hasProcessTestEvent(storeFake.events, "run.ready_for_review") {
			t.Fatalf("runner disconnect events=%+v", storeFake.events)
		}
	})

	t.Run("outbound workspace transfer failure blocks execution", func(t *testing.T) {
		repo := initProcessTestRepository(t)
		safe := processTestSafeContext(repo)
		safe.Runtime = executioncontext.RuntimeContext{}
		storeFake := &runnerSyncStore{}
		client := &runnerOutboundFailureClient{
			successfulSyncClient: &successfulSyncClient{},
			err:                  errors.New("runner transfer disconnected"),
		}
		processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)
		run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}

		result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
		if err != nil {
			t.Fatal(err)
		}
		if result.RunStatus != "FAILED" {
			t.Fatalf("transfer failure result=%+v", result)
		}
		if len(client.directions) != 1 || client.directions[0] != "to_runner" {
			t.Fatalf("transfer directions=%v", client.directions)
		}
		if !hasProcessTestEvent(storeFake.events, "workspace.transfer.failed") || hasProcessTestEvent(storeFake.events, "agent.message") || hasProcessTestEvent(storeFake.events, "run.ready_for_review") {
			t.Fatalf("transfer failure events=%+v", storeFake.events)
		}
	})

	t.Run("engine failure still syncs authoritative workspace", func(t *testing.T) {
		repo := initProcessTestRepository(t)
		safe := processTestSafeContext(repo)
		storeFake := &runnerSyncStore{}
		client := &successfulSyncClient{payload: runnerTransferPayload(t, repo)}
		processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)
		registry, err := engine.NewRegistry(processTestEngine{workspace: repo, fail: errors.New("engine failed")})
		if err != nil {
			t.Fatal(err)
		}
		processor.engines = registry
		run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

		result, err := processor.runEngineOnRunner(t.Context(), run, safe, "runner-1", "session-1")
		if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if len(client.directions) != 1 || client.directions[0] != "from_runner" || !client.confirmed {
			t.Fatalf("engine failure did not complete sync-back: directions=%v confirmed=%v", client.directions, client.confirmed)
		}
		if !hasProcessTestEvent(storeFake.events, "run.failed") || hasProcessTestEvent(storeFake.events, "run.ready_for_review") {
			t.Fatalf("terminal events=%+v", storeFake.events)
		}
	})

	t.Run("invalid returned workspace fails without acknowledgement", func(t *testing.T) {
		repo := initProcessTestRepository(t)
		safe := processTestSafeContext(repo)
		safe.Runner = &executioncontext.RunnerContext{ID: "runner-1"}
		storeFake := &runnerSyncStore{}
		client := &successfulSyncClient{payload: []byte("not a git bundle")}
		processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)
		run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

		result, err := processor.runEngineOnRunner(t.Context(), run, safe, "runner-1", "session-1")
		if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if client.confirmed {
			t.Fatal("invalid returned workspace was acknowledged as applied")
		}
		if !hasProcessTestEvent(storeFake.events, "workspace.transfer.failed") || !hasProcessTestEvent(storeFake.events, "run.failed") || hasProcessTestEvent(storeFake.events, "run.ready_for_review") {
			t.Fatalf("terminal events=%+v", storeFake.events)
		}
	})

	t.Run("parent cancellation syncs back before returning cancellation", func(t *testing.T) {
		repo := initProcessTestRepository(t)
		safe := processTestSafeContext(repo)
		safe.Runner = &executioncontext.RunnerContext{ID: "runner-1"}
		storeFake := &runnerSyncStore{}
		client := &successfulSyncClient{payload: runnerTransferPayload(t, repo)}
		processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)
		run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result, err := processor.runEngineOnRunner(ctx, run, safe, "runner-1", "session-1")
		if !errors.Is(err, context.Canceled) || result.RunStatus != "" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if len(client.directions) != 1 || client.directions[0] != "from_runner" || !client.confirmed {
			t.Fatalf("cancelled run did not complete sync-back: directions=%v confirmed=%v", client.directions, client.confirmed)
		}
	})

	t.Run("candidate snapshot failure blocks review ready", func(t *testing.T) {
		repo := initProcessTestRepository(t)
		safe := processTestSafeContext(repo)
		safe.Runner = &executioncontext.RunnerContext{ID: "runner-1"}
		storeFake := &runnerSyncStore{}
		client := &successfulSyncClient{payload: runnerTransferPayload(t, repo)}
		processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)
		failureStore := &runnerSnapshotFailureStore{runnerSyncStore: storeFake, err: errors.New("snapshot persistence failed")}
		blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := evidence.NewCandidateSnapshotter(evidence.NewCandidateCollector(), failureStore, blobs)
		if err != nil {
			t.Fatal(err)
		}
		processor.candidate = candidate
		run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}

		result, err := processor.runEngineOnRunner(t.Context(), run, safe, "runner-1", "session-1")
		if err != nil || result.RunStatus != "FAILED" || result.FailureReason == nil {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if !hasProcessTestEvent(storeFake.events, "run.failed") || hasProcessTestEvent(storeFake.events, "run.ready_for_review") {
			t.Fatalf("terminal events=%+v", storeFake.events)
		}
	})
}
