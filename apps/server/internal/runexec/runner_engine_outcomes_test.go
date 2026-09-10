package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runnerSnapshotFailureStore struct {
	*runnerSyncStore
	err error
}

func (s *runnerSnapshotFailureStore) CreateArtifact(context.Context, store.Artifact) (store.Artifact, error) {
	return store.Artifact{}, s.err
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

	t.Run("parent cancellation syncs back before returning cancellation", func(t *testing.T) {
		repo := initProcessTestRepository(t)
		safe := processTestSafeContext(repo)
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
