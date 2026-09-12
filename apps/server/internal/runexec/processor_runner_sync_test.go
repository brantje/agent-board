package runexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func (s *runnerSyncStore) GetRunner(_ context.Context, id string) (store.Runner, error) {
	return store.Runner{ID: id, Name: "External test runner"}, nil
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

func (c *failingSyncClient) ConfirmTransferApplied(context.Context, string, string) error { return nil }

type successfulSyncClient struct {
	payload    []byte
	confirmed  bool
	confirmErr error
	directions []string
}

func (c *successfulSyncClient) SendTransfer(_ context.Context, _, _, direction string, payload []byte, progress runner.TransferProgressFunc) error {
	c.directions = append(c.directions, direction)
	if progress != nil && len(payload) > 0 {
		progress(runner.TransferProgress{BytesTransferred: int64(len(payload)), TotalBytes: int64(len(payload))})
	}
	return nil
}

func (c *successfulSyncClient) ReceiveTransfer(_ context.Context, _ string, progress runner.TransferProgressFunc) (string, []byte, error) {
	if progress != nil {
		progress(runner.TransferProgress{BytesTransferred: int64(len(c.payload)), TotalBytes: int64(len(c.payload))})
	}
	return "returned-transfer", c.payload, nil
}

func (c *successfulSyncClient) ConfirmTransferApplied(context.Context, string, string) error {
	if c.confirmErr != nil {
		return c.confirmErr
	}
	c.confirmed = true
	return nil
}

type runnerSyncConnector struct {
	client runnerClient
}

func (c runnerSyncConnector) Connect(context.Context, string, string) (runnerClient, error) {
	return c.client, nil
}

type staticWorkspaceEnsurer struct {
	workspace store.Workspace
}

func (e staticWorkspaceEnsurer) EnsureIssueWorkspace(context.Context, string, string) (store.Workspace, error) {
	return e.workspace, nil
}

func newRunnerSyncProcessor(t *testing.T, repo string, safe executioncontext.SafeContext, storeFake *runnerSyncStore, client runnerClient) *Processor {
	t.Helper()
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
	engines, err := engine.NewRegistry(processTestEngine{workspace: repo})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewProcessor(
		storeFake,
		processTestResolver{resolved: executioncontext.Resolved{Safe: safe}},
		nil,
		&runnerSyncSessions{},
		engines,
		recorder,
		output,
		git,
		runnerSyncConnector{client: client},
	)
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

func runnerTransferPayload(t *testing.T, authoritative string) []byte {
	t.Helper()
	runnerRepo := filepath.Join(t.TempDir(), "runner")
	cmd := exec.Command("git", "clone", "-q", authoritative, runnerRepo)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(runnerRepo, "returned.txt"), []byte("from runner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=Agent Board Test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "runner result"}} {
		cmd := exec.Command("git", append([]string{"-C", runnerRepo}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := git.TransferSnapshot(t.Context(), runnerRepo, "returned-transfer")
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestRunnerSyncBackFailureOverridesEngineSuccess(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	storeFake := &runnerSyncStore{}
	client := &failingSyncClient{}
	processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)

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

func TestRunnerExecutionCompletesWithWorkspaceSyncAndReviewEvidence(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	storeFake := &runnerSyncStore{}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, repo)}
	processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)
	processor.SetWorkspaceEnsurer(staticWorkspaceEnsurer{workspace: store.Workspace{
		ID: safe.Workspace.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, Path: repo, WorkingBranch: safe.Workspace.WorkingBranch, BootstrapStatus: "READY",
	}})

	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "READY_FOR_REVIEW" {
		t.Fatalf("result=%+v", result)
	}
	if body, err := os.ReadFile(filepath.Join(repo, "returned.txt")); err != nil || string(body) != "from runner\n" {
		t.Fatalf("returned workspace file=%q err=%v", body, err)
	}
	if !client.confirmed || len(client.directions) != 2 || client.directions[0] != "to_runner" || client.directions[1] != "from_runner" {
		t.Fatalf("confirmed=%v directions=%v", client.confirmed, client.directions)
	}
	for _, eventType := range []string{"workspace.transfer.started", "workspace.transfer.progress", "workspace.transfer.completed", "agent.message", "run.ready_for_review"} {
		if !hasProcessTestEvent(storeFake.events, eventType) {
			t.Fatalf("missing event %q in %+v", eventType, storeFake.events)
		}
	}
	if event := processTestEvent(storeFake.events, "run.ready_for_review"); event.RuntimeInstanceID != nil {
		t.Fatalf("runner-owned review evidence referenced Runtime Instance: %+v", event)
	}
}

func TestRunnerLiveSessionResumesWithoutRetransferringWorkspace(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	storeFake := &runnerSyncStore{}
	storeFake.sessions = []store.ExecutionSession{{
		ID: "session-live", ProjectID: safe.Project.ID, RunID: safe.Run.ID, RunnerID: "runner-1", Status: "RUNNING",
	}}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, repo)}
	processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)

	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "RUNNING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "READY_FOR_REVIEW" {
		t.Fatalf("result=%+v", result)
	}
	if len(client.directions) != 1 || client.directions[0] != "from_runner" {
		t.Fatalf("reconciled runner transfer directions=%v", client.directions)
	}
	if hasProcessTestEvent(storeFake.events, "run.started") || hasProcessTestEvent(storeFake.events, "runtime.provisioning") {
		t.Fatalf("reconciled runner execution restarted provisioning: %+v", storeFake.events)
	}
	if !hasProcessTestEvent(storeFake.events, "run.resumed") || !hasProcessTestEvent(storeFake.events, "run.ready_for_review") {
		t.Fatalf("reconciled runner execution events=%+v", storeFake.events)
	}
}

func TestRunnerSyncBackAcknowledgementFailureFailsRun(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	storeFake := &runnerSyncStore{}
	client := &successfulSyncClient{
		payload:    runnerTransferPayload(t, repo),
		confirmErr: errors.New("ack transport lost"),
	}
	processor := newRunnerSyncProcessor(t, repo, safe, storeFake, client)

	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}
	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "FAILED" {
		t.Fatalf("missing transfer-applied acknowledgement became success: %+v", result)
	}
	if client.confirmed || !hasProcessTestEvent(storeFake.events, "workspace.transfer.failed") || hasProcessTestEvent(storeFake.events, "run.ready_for_review") {
		t.Fatalf("acknowledgement failure state confirmed=%v events=%+v", client.confirmed, storeFake.events)
	}
	if body, err := os.ReadFile(filepath.Join(repo, "returned.txt")); err != nil || string(body) != "from runner\n" {
		t.Fatalf("returned workspace must remain applied after lost acknowledgement: file=%q err=%v", body, err)
	}
}

func TestRunnerSyncBackAppliesWorkspaceAndAcknowledgesTransfer(t *testing.T) {
	authoritative := initProcessTestRepository(t)
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	storeFake := &runnerSyncStore{}
	recorder, err := evidence.NewRecorder(storeFake, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, authoritative)}
	processor := &Processor{store: storeFake, events: recorder, git: git, runners: runnerSyncConnector{client: client}}
	safe := processTestSafeContext(authoritative)
	safe.Runtime = executioncontext.RuntimeContext{}

	if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(authoritative, "returned.txt"))
	if err != nil || string(body) != "from runner\n" {
		t.Fatalf("returned workspace file=%q err=%v", body, err)
	}
	if !client.confirmed || !hasProcessTestEvent(storeFake.events, "workspace.transfer.completed") {
		t.Fatalf("confirmed=%v events=%+v", client.confirmed, storeFake.events)
	}
}

func TestRunnerSyncBackTransportFailuresDoNotApplyOrAcknowledge(t *testing.T) {
	t.Run("missing prepared session", func(t *testing.T) {
		authoritative := initProcessTestRepository(t)
		safe := processTestSafeContext(authoritative)
		safe.Runtime = executioncontext.RuntimeContext{}
		storeFake := &runnerSyncStore{}
		client := &successfulSyncClient{}
		processor := newRunnerSyncProcessor(t, authoritative, safe, storeFake, client)

		if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", ""); err == nil {
			t.Fatal("sync-back succeeded without a prepared runner session")
		}
		if len(client.directions) != 0 || client.confirmed {
			t.Fatalf("missing session touched runner transport: directions=%v confirmed=%v", client.directions, client.confirmed)
		}
	})

	t.Run("runner disconnects before sync request", func(t *testing.T) {
		authoritative := initProcessTestRepository(t)
		safe := processTestSafeContext(authoritative)
		safe.Runtime = executioncontext.RuntimeContext{}
		storeFake := &runnerSyncStore{}
		client := &successfulSyncClient{}
		processor := newRunnerSyncProcessor(t, authoritative, safe, storeFake, client)
		processor.runners = failingRunnerSyncConnector{err: errors.New("runner disconnected before sync-back")}

		if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); err == nil {
			t.Fatal("sync-back succeeded after runner disconnect")
		}
		if len(client.directions) != 0 || client.confirmed {
			t.Fatalf("disconnected sync touched stale runner client: directions=%v confirmed=%v", client.directions, client.confirmed)
		}
		if _, err := os.Stat(filepath.Join(authoritative, "returned.txt")); !os.IsNotExist(err) {
			t.Fatalf("disconnected runner modified authoritative Workspace: %v", err)
		}
		if !hasProcessTestEvent(storeFake.events, "workspace.transfer.failed") || hasProcessTestEvent(storeFake.events, "workspace.transfer.completed") {
			t.Fatalf("unexpected disconnect transfer events=%+v", storeFake.events)
		}
	})

	t.Run("runner disconnects while requesting sync", func(t *testing.T) {
		authoritative := initProcessTestRepository(t)
		safe := processTestSafeContext(authoritative)
		safe.Runtime = executioncontext.RuntimeContext{}
		storeFake := &runnerSyncStore{}
		client := &runnerOutboundFailureClient{
			successfulSyncClient: &successfulSyncClient{},
			err:                  errors.New("runner disconnected while requesting sync-back"),
		}
		processor := newRunnerSyncProcessor(t, authoritative, safe, storeFake, client)

		if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); err == nil {
			t.Fatal("sync-back succeeded after sync request transport failure")
		}
		if len(client.directions) != 1 || client.directions[0] != "from_runner" || client.confirmed {
			t.Fatalf("sync request failure state directions=%v confirmed=%v", client.directions, client.confirmed)
		}
		if _, err := os.Stat(filepath.Join(authoritative, "returned.txt")); !os.IsNotExist(err) {
			t.Fatalf("failed sync request modified authoritative Workspace: %v", err)
		}
		if !hasProcessTestEvent(storeFake.events, "workspace.transfer.failed") || hasProcessTestEvent(storeFake.events, "workspace.transfer.completed") {
			t.Fatalf("unexpected sync request failure events=%+v", storeFake.events)
		}
	})
}

func TestRunnerSyncBackWorkspaceLockFailureDoesNotApplyOrAcknowledge(t *testing.T) {
	authoritative := initProcessTestRepository(t)
	safe := processTestSafeContext(authoritative)
	safe.Runtime = executioncontext.RuntimeContext{}
	storeFake := &runnerSyncStore{}
	client := &successfulSyncClient{payload: runnerTransferPayload(t, authoritative)}
	processor := newRunnerSyncProcessor(t, authoritative, safe, storeFake, client)
	processor.store = &runnerWorkspaceLockFailureStore{
		runnerSyncStore: storeFake,
		err:             errors.New("workspace already has an execution writer"),
	}

	if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); err == nil {
		t.Fatal("sync-back succeeded without authoritative Workspace writer lock")
	}
	if client.confirmed {
		t.Fatal("unapplied Workspace transfer was acknowledged")
	}
	if len(client.directions) != 1 || client.directions[0] != "from_runner" {
		t.Fatalf("unexpected transfer directions=%v", client.directions)
	}
	if _, err := os.Stat(filepath.Join(authoritative, "returned.txt")); !os.IsNotExist(err) {
		t.Fatalf("returned Workspace was applied without writer lock: %v", err)
	}
	if !hasProcessTestEvent(storeFake.events, "workspace.transfer.failed") || hasProcessTestEvent(storeFake.events, "workspace.transfer.completed") {
		t.Fatalf("unexpected transfer events=%+v", storeFake.events)
	}
}

type runnerQuestionStore struct {
	*orchestrationQuestionStore
	open store.Question
}

func (s *runnerQuestionStore) GetOpenBlockingQuestion(context.Context, string, string) (store.Question, error) {
	if s.open.ID == "" {
		return store.Question{}, store.ErrNotFound
	}
	return s.open, nil
}

func TestRunnerWaitingForInputKeepsRunWaitingWithoutRuntimeCleanup(t *testing.T) {
	base := &processTestStore{}
	questionStore := &runnerQuestionStore{
		orchestrationQuestionStore: &orchestrationQuestionStore{processTestStore: base},
		open:                       store.Question{ID: "question-1", Blocking: true, Status: "OPEN"},
	}
	recorder, err := evidence.NewRecorder(questionStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{store: questionStore, events: recorder}
	safe := continuationSafeContext()

	result, err := processor.finishWaitingForInputRunner(t.Context(), safe)
	if err != nil || result.RunStatus != "WAITING_FOR_INPUT" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if event := processTestEvent(base.events, "run.waiting_for_input"); event.Type == "" || event.RuntimeInstanceID != nil {
		t.Fatalf("waiting event=%+v", event)
	}
}
