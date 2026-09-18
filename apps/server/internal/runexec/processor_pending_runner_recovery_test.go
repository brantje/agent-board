package runexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

type pendingRecoveryEngine struct{}

func (pendingRecoveryEngine) Name() string { return "pending-recovery" }

func (pendingRecoveryEngine) Execute(ctx context.Context, request engine.Request) (engine.Result, error) {
	if request.DelegationContinuation == nil {
		return engine.Result{}, errors.New("missing delegation continuation")
	}
	process, err := request.Launcher.Start(ctx, engine.ProcessRequest{
		Kind: "tool", Name: "pending-recovery", Command: []string{"agent"},
	})
	if err != nil {
		return engine.Result{}, err
	}
	if _, err := process.Wait(ctx); err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Summary: "parent resumed after delegated work"}, nil
}

type materializingTransferClient struct {
	workspacePath string
	directions    []string
}

func (c *materializingTransferClient) SendTransfer(ctx context.Context, _, transferID, direction string, payload []byte, progress runner.TransferProgressFunc) error {
	c.directions = append(c.directions, direction)
	if direction != "to_runner" {
		return nil
	}
	if progress != nil {
		progress(runner.TransferProgress{BytesTransferred: int64(len(payload)), TotalBytes: int64(len(payload))})
	}
	if err := os.RemoveAll(c.workspacePath); err != nil {
		return err
	}
	bundlePath := filepath.Join(filepath.Dir(c.workspacePath), transferID+".bundle")
	if err := os.WriteFile(bundlePath, payload, 0o600); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "-q", bundlePath, c.workspacePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return errors.New("materialize transferred Workspace: " + err.Error() + ": " + string(output))
	}
	return nil
}

func (c *materializingTransferClient) ReceiveTransfer(ctx context.Context, _ string, progress runner.TransferProgressFunc) (string, []byte, error) {
	git, err := workspace.NewGitCLI("")
	if err != nil {
		return "", nil, err
	}
	payload, err := git.TransferSnapshot(ctx, c.workspacePath, "pending-recovery-return")
	if err != nil {
		return "", nil, err
	}
	if progress != nil {
		progress(runner.TransferProgress{BytesTransferred: int64(len(payload)), TotalBytes: int64(len(payload))})
	}
	return "pending-recovery-return", payload, nil
}

func (*materializingTransferClient) ConfirmTransferApplied(context.Context, string, string) error {
	return nil
}

type workspaceCheckingRunnerClient struct {
	*launcherClient
	workspacePath string
	started       bool
}

func (c *workspaceCheckingRunnerClient) Start(ctx context.Context, sessionID string, request runner.Request) (runner.ProcessSession, error) {
	body, err := os.ReadFile(filepath.Join(c.workspacePath, "delegated.txt"))
	if err != nil {
		return nil, errors.New("runner execution started before delegated Workspace was prepared: " + err.Error())
	}
	if string(body) != "delegated workspace state\n" {
		return nil, errors.New("runner execution observed stale delegated Workspace contents")
	}
	c.started = true
	return c.launcherClient.Start(ctx, sessionID, request)
}

func TestRecoveredPendingRunnerResumePreparesCurrentWorkspaceBeforeExecution(t *testing.T) {
	repository := initProcessTestRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "delegated.txt"), []byte("delegated workspace state\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "delegated.txt"},
		{"-c", "user.name=Agent Board Test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "delegated workspace"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repository}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	revisionOutput, err := exec.Command("git", "-C", repository, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.TrimSpace(string(revisionOutput))

	safe := processTestSafeContext(repository)
	safe.Runtime = executioncontext.RuntimeContext{}
	safe.Agent.Engine = "pending-recovery"
	parentAgentID := safe.Agent.ID
	run := store.Run{
		ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID,
		WorkspaceID: safe.Workspace.ID, AgentID: &parentAgentID, Status: "STARTING",
	}
	outcome := store.DelegationOutcomeSucceeded
	summary := "delegate updated the Workspace"
	accepted := true
	completedAt := time.Now().UTC()

	base := &runnerSyncStore{}
	base.delegationContinuation = store.Delegation{
		ID: "delegation-1", ProjectID: run.ProjectID, IssueID: run.IssueID,
		ParentRunID: run.ID, ParentAgentID: parentAgentID, TargetAgentID: "delegate-agent",
		Task: "update delegated.txt", DelegatedRunID: "child-run",
		Outcome: &outcome, ResultSummary: &summary, WorkspaceChangesAccepted: &accepted, CompletedAt: &completedAt,
	}
	base.workspaceRevision = revision
	sessionStore := &launcherSessionStore{
		run: run,
		sessions: map[string]store.ExecutionSession{
			"session-pending": {
				ID: "session-pending", ProjectID: run.ProjectID, RunID: run.ID,
				RunnerID: "runner-1", Status: "PENDING",
			},
		},
	}
	storeFake := &uncertaintyExecutionStore{runnerSyncStore: base, launcherSessionStore: sessionStore}
	runnerWorkspace := filepath.Join(t.TempDir(), "runner-workspace")
	transport := &workspaceCheckingRunnerClient{
		launcherClient: newLauncherClient("", "", 0, nil),
		workspacePath:  runnerWorkspace,
	}
	executionSessions, err := newLauncherExecutionSessionService(storeFake, transport)
	if err != nil {
		t.Fatal(err)
	}
	authorizedSessions, err := app.NewAuthorizedExecutionSessionService(executionSessions, launcherPreparer{})
	if err != nil {
		t.Fatal(err)
	}
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
	engines, err := engine.NewRegistry(pendingRecoveryEngine{})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	transfer := &materializingTransferClient{workspacePath: runnerWorkspace}
	processor, err := NewProcessor(
		storeFake,
		processTestResolver{resolved: executioncontext.Resolved{Safe: safe}},
		nil,
		authorizedSessions,
		engines,
		recorder,
		output,
		git,
		runnerSyncConnector{client: transfer},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := processor.Process(t.Context(), &store.SchedulerAdmission{
		RunnerID: "runner-1",
		Job: store.SchedulerJob{
			ID: "resume-job", ProjectID: run.ProjectID, RunID: run.ID, Kind: "RESUME", State: "CLAIMED",
		},
		Lease: store.SchedulerLease{JobID: "resume-job", LeaseToken: "lease"},
		Run:   run,
	}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "READY_FOR_REVIEW" {
		t.Fatalf("result=%+v", result)
	}
	if !transport.started {
		t.Fatal("runner execution never started")
	}
	if len(transfer.directions) != 2 || transfer.directions[0] != "to_runner" || transfer.directions[1] != "from_runner" {
		t.Fatalf("workspace transfer directions=%v want outbound preparation before sync-back", transfer.directions)
	}
	body, err := os.ReadFile(filepath.Join(runnerWorkspace, "delegated.txt"))
	if err != nil || string(body) != "delegated workspace state\n" {
		t.Fatalf("runner Workspace delegated contents=%q err=%v", body, err)
	}
}
