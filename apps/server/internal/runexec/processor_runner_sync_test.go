package runexec

import (
	"context"
	"errors"
	"os"
	"strings"
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
	lockErr error
}

func (s *runnerSyncStore) GetRunner(_ context.Context, id string) (store.Runner, error) {
	return store.Runner{ID: id, Name: "External host"}, nil
}

func (s *runnerSyncStore) AcquireWorkspaceBootstrapLock(context.Context, string) (store.WorkspaceBootstrapLock, error) {
	if s.lockErr != nil {
		return nil, s.lockErr
	}
	return noopWorkspaceLock{}, nil
}

type noopWorkspaceLock struct{}

func (noopWorkspaceLock) Release() error { return nil }

type runnerSyncSessions struct {
	processTestSessions
	created store.ExecutionSession
}

func (s *runnerSyncSessions) CreateRunnerSession(_ context.Context, projectID, runID, runnerID string) (store.ExecutionSession, error) {
	s.created = store.ExecutionSession{ID: "session-1", ProjectID: projectID, RunID: runID, RunnerID: runnerID, Status: "PENDING"}
	return s.created, nil
}

type recordingSyncClient struct {
	directions []string
	applied    []string
	receiveErr error
	confirmErr error
	receive    func(context.Context) (string, []byte, error)
}

func (c *recordingSyncClient) SendTransfer(_ context.Context, _, _, direction string, _ []byte, _ runner.TransferProgressFunc) error {
	c.directions = append(c.directions, direction)
	return nil
}

func (c *recordingSyncClient) ReceiveTransfer(ctx context.Context, _ string, _ runner.TransferProgressFunc) (string, []byte, error) {
	if c.receive != nil {
		return c.receive(ctx)
	}
	if c.receiveErr != nil {
		return "", nil, c.receiveErr
	}
	return "", nil, errors.New("checksum mismatch")
}

func (c *recordingSyncClient) ConfirmTransferApplied(_ context.Context, sessionID, transferID string) error {
	c.applied = append(c.applied, sessionID+"/"+transferID)
	return c.confirmErr
}

type recordingSyncConnector struct{ client runnerClient }

func (c recordingSyncConnector) Connect(context.Context, string, string) (runnerClient, error) {
	return c.client, nil
}

type waitingTestEngine struct{}

func (waitingTestEngine) Name() string { return "test" }
func (waitingTestEngine) Execute(context.Context, engine.Request) (engine.Result, error) {
	return engine.Result{}, engine.ErrWaitingForInput
}

type cancellingTestEngine struct{ cancel context.CancelFunc }

func (c cancellingTestEngine) Name() string { return "test" }
func (c cancellingTestEngine) Execute(context.Context, engine.Request) (engine.Result, error) {
	c.cancel()
	return engine.Result{}, context.Canceled
}

type waitingRunnerStore struct {
	*runnerSyncStore
	*questionTestStore
}

func TestRunnerSyncBackFailureOverridesEngineSuccess(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	evidenceStore := &runnerSyncStore{}
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
	registry, err := engine.NewRegistry(processTestEngine{workspace: repo})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingSyncClient{receiveErr: errors.New("checksum mismatch")}
	processor, err := NewProcessor(evidenceStore, processTestResolver{resolved: executioncontext.Resolved{Safe: safe}}, nil, &runnerSyncSessions{}, registry, recorder, output, candidate, git, recordingSyncConnector{client: client})
	if err != nil {
		t.Fatal(err)
	}
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "FAILED" {
		t.Fatalf("result=%+v", result)
	}
	if !hasProcessTestEvent(evidenceStore.events, "workspace.transfer.failed") {
		t.Fatalf("missing sync-back failure event in %+v", evidenceStore.events)
	}
	if hasProcessTestEvent(evidenceStore.events, "run.ready_for_review") {
		t.Fatal("sync-back failure became READY_FOR_REVIEW")
	}
	if len(evidenceStore.provenance) == 0 {
		t.Fatal("runner provenance was not persisted")
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatal(err)
	}
	if len(client.directions) < 2 || client.directions[len(client.directions)-1] != "from_runner" {
		t.Fatalf("sync-back must request a from_runner snapshot, directions=%v", client.directions)
	}
}

func TestRunnerEngineFailureIsNotMaskedBySyncBackFailure(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	evidenceStore := &runnerSyncStore{}
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
	registry, err := engine.NewRegistry(processTestEngine{workspace: repo, fail: errors.New("native session did not start")})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingSyncClient{receiveErr: errors.New("checksum mismatch")}
	processor, err := NewProcessor(evidenceStore, processTestResolver{resolved: executioncontext.Resolved{Safe: safe}}, nil, &runnerSyncSessions{}, registry, recorder, output, candidate, git, recordingSyncConnector{client: client})
	if err != nil {
		t.Fatal(err)
	}
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, "native session did not start") {
		t.Fatalf("engine failure was masked: %+v", result)
	}
}

func TestRunnerCancellationStillAttemptsWorkspaceSyncBack(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	evidenceStore := &runnerSyncStore{}
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
	ctx, cancel := context.WithCancel(context.Background())
	registry, err := engine.NewRegistry(cancellingTestEngine{cancel: cancel})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingSyncClient{receiveErr: errors.New("recovery snapshot unavailable")}
	processor, err := NewProcessor(evidenceStore, processTestResolver{resolved: executioncontext.Resolved{Safe: safe}}, nil, &runnerSyncSessions{}, registry, recorder, output, candidate, git, recordingSyncConnector{client: client})
	if err != nil {
		t.Fatal(err)
	}
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	_, err = processor.Process(ctx, &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("process error=%v", err)
	}
	if len(client.directions) < 2 || client.directions[len(client.directions)-1] != "from_runner" {
		t.Fatalf("cancellation skipped recovery sync: directions=%v", client.directions)
	}
	if !hasProcessTestEvent(evidenceStore.events, "workspace.transfer.failed") {
		t.Fatal("cancellation recovery failure was not recorded")
	}
}

func TestRunnerWaitingForInputSkipsWorkspaceSyncBack(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	question := store.Question{ID: "question-1", ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, RunID: safe.Run.ID, Blocking: true, Status: "OPEN"}
	evidenceStore := &waitingRunnerStore{runnerSyncStore: &runnerSyncStore{}, questionTestStore: &questionTestStore{open: &question}}
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
	registry, err := engine.NewRegistry(waitingTestEngine{})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingSyncClient{receive: func(ctx context.Context) (string, []byte, error) {
		<-ctx.Done()
		return "", nil, ctx.Err()
	}}
	processor, err := NewProcessor(evidenceStore, processTestResolver{resolved: executioncontext.Resolved{Safe: safe}}, nil, &runnerSyncSessions{}, registry, recorder, output, candidate, git, recordingSyncConnector{client: client})
	if err != nil {
		t.Fatal(err)
	}
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "WAITING_FOR_INPUT" {
		t.Fatalf("result=%+v", result)
	}
	for _, direction := range client.directions {
		if direction == "from_runner" {
			t.Fatalf("WAITING_FOR_INPUT requested snapshot while the process is still live: %v", client.directions)
		}
	}
}

type failingConnectConnector struct{ err error }

func (c failingConnectConnector) Connect(context.Context, string, string) (runnerClient, error) {
	return nil, c.err
}

type failingSendClient struct {
	recordingSyncClient
	sendErr error
}

func (c *failingSendClient) SendTransfer(context.Context, string, string, string, []byte, runner.TransferProgressFunc) error {
	return c.sendErr
}

func processorForTransferTests(t *testing.T, store *runnerSyncStore, git workspace.Git, connector RunnerConnector) *Processor {
	t.Helper()
	blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(store, blobs, 64)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := evidence.NewCandidateSnapshotter(evidence.NewCandidateCollector(), store, blobs)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := engine.NewRegistry(processTestEngine{workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewProcessor(store, processTestResolver{}, nil, &runnerSyncSessions{}, registry, recorder, output, candidate, git, connector)
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

type stubGit struct{ workspace.Git }

func TestTransferAndSyncWorkspaceRequireRunnerDependencies(t *testing.T) {
	ctx := context.Background()
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	evidenceStore := &runnerSyncStore{}
	processor := processorForTransferTests(t, evidenceStore, git, nil)
	processor.git = git
	if err := processor.transferWorkspaceToRunner(ctx, safe, "runner-1", "session-1"); err == nil {
		t.Fatal("nil connector accepted")
	}
	if err := processor.syncWorkspaceFromRunner(ctx, safe, "runner-1", ""); err == nil {
		t.Fatal("blank session sync accepted")
	}
	processor.git = stubGit{}
	processor.runners = recordingSyncConnector{client: &recordingSyncClient{}}
	if err := processor.transferWorkspaceToRunner(ctx, safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "git transfer") {
		t.Fatalf("expected git transfer error, got %v", err)
	}
	if err := processor.syncWorkspaceFromRunner(ctx, safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "git transfer") {
		t.Fatalf("expected git transfer error, got %v", err)
	}

	locked := processorForTransferTests(t, &runnerSyncStore{lockErr: errors.New("lock busy")}, git, failingConnectConnector{err: errors.New("disconnected")})
	if err := locked.transferWorkspaceToRunner(ctx, safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "lock busy") {
		t.Fatalf("expected lock error, got %v", err)
	}
	if err := locked.syncWorkspaceFromRunner(ctx, safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "disconnected") {
		t.Fatalf("expected connect error, got %v", err)
	}

	sendFail := processorForTransferTests(t, &runnerSyncStore{}, git, recordingSyncConnector{client: &failingSendClient{sendErr: errors.New("write failed")}})
	if err := sendFail.transferWorkspaceToRunner(ctx, safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected send error, got %v", err)
	}
	if err := sendFail.syncWorkspaceFromRunner(ctx, safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected send error, got %v", err)
	}
	result, err := sendFail.finishWaitingForInputRunner(ctx, safe)
	if err != nil || result.RunStatus != "FAILED" {
		t.Fatalf("missing question store: %+v err=%v", result, err)
	}
}
