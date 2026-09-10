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
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

type runnerSyncStore struct {
	processTestStore
}

func (s *runnerSyncStore) AcquireWorkspaceExecutionLock(context.Context, string, string) (store.WorkspaceBootstrapLock, error) {
	return noopWorkspaceLock{}, nil
}

type noopWorkspaceLock struct{}

func (noopWorkspaceLock) Release() error { return nil }

type runnerSyncSessions struct {
	processTestSessions
}

func (s *runnerSyncSessions) CreateRunnerSession(_ context.Context, projectID, runID, runnerID string) (store.ExecutionSession, error) {
	return store.ExecutionSession{ID: "session-1", ProjectID: projectID, RunID: runID, RunnerID: runnerID, Status: "PENDING"}, nil
}

type failingSyncClient struct {
	directions []string
}

func (c *failingSyncClient) SendTransfer(_ context.Context, _, _, direction string, _ []byte, _ runner.TransferProgressFunc) error {
	c.directions = append(c.directions, direction)
	return nil
}

func (c *failingSyncClient) ReceiveTransfer(context.Context, string, runner.TransferProgressFunc) (string, []byte, error) {
	return "", nil, errors.New("sync-back failed")
}

func (c *failingSyncClient) ConfirmTransferApplied(context.Context, string, string) error {
	return nil
}

type runnerSyncConnector struct {
	client runnerClient
}

func (c runnerSyncConnector) Connect(context.Context, string, string) (runnerClient, error) {
	return c.client, nil
}

func TestRunnerSyncBackFailureOverridesEngineSuccess(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	storeFake := &runnerSyncStore{}

	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(storeFake, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(storeFake, blobs, 64)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := evidence.NewCandidateSnapshotter(evidence.NewCandidateCollector(), storeFake, blobs)
	if err != nil {
		t.Fatal(err)
	}
	engines, err := engine.NewRegistry(processTestEngine{workspace: repo})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	client := &failingSyncClient{}
	processor, err := NewProcessor(
		storeFake,
		processTestResolver{resolved: executioncontext.Resolved{Safe: safe}},
		nil,
		&runnerSyncSessions{},
		engines,
		recorder,
		output,
		candidate,
		git,
		runnerSyncConnector{client: client},
	)
	if err != nil {
		t.Fatal(err)
	}

	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "FAILED" {
		t.Fatalf("sync-back failure became success: %+v", result)
	}
	if !hasProcessTestEvent(storeFake.events, "workspace.transfer.failed") || hasProcessTestEvent(storeFake.events, "run.ready_for_review") {
		t.Fatalf("unexpected terminal events: %+v", storeFake.events)
	}
	if len(client.directions) != 2 || client.directions[0] != "to_runner" || client.directions[1] != "from_runner" {
		t.Fatalf("workspace transfer directions=%v", client.directions)
	}
}
