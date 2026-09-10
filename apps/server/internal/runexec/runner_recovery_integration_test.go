package runexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func TestSyncWorkspaceFromRunnerApplyFailureDoesNotAcknowledge(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingSyncClient{receive: func(context.Context) (string, []byte, error) {
		return "broken-transfer", []byte("not a git bundle"), nil
	}}
	store := &runnerSyncStore{}
	processor := processorForTransferTests(t, store, git, recordingSyncConnector{client: client})
	if err := processor.syncWorkspaceFromRunner(context.Background(), safe, "runner-1", "session-1"); err == nil {
		t.Fatal("invalid returned bundle unexpectedly applied")
	}
	if len(client.applied) != 0 {
		t.Fatalf("apply failure emitted acknowledgement: %v", client.applied)
	}
	if !hasProcessTestEvent(store.events, "workspace.transfer.failed") {
		t.Fatal("apply failure was not recorded")
	}
}

func TestRunnerCancellationRecoversReturnedWorkspaceChanges(t *testing.T) {
	repo := initProcessTestRepository(t)
	returned := runnerRecoveryBundle(t, "recovered-before-cancel.txt", "cancelled edit")
	ctx, cancel := context.WithCancel(context.Background())
	processor, evidenceStore, client := runnerRecoveryProcessor(t, repo, cancellingTestEngine{cancel: cancel}, returned)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}

	_, err := processor.Process(ctx, &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("process error=%v want context.Canceled", err)
	}
	content, err := os.ReadFile(filepath.Join(repo, "recovered-before-cancel.txt"))
	if err != nil {
		t.Fatalf("recovered cancellation edit: %v", err)
	}
	if string(content) != "cancelled edit" {
		t.Fatalf("recovered cancellation content=%q", content)
	}
	if len(client.applied) != 1 {
		t.Fatalf("recovered cancellation acknowledgements=%v", client.applied)
	}
	if hasProcessTestEvent(evidenceStore.events, "run.ready_for_review") {
		t.Fatal("cancelled recovery became READY_FOR_REVIEW")
	}
}

func TestRunnerEngineFailureRecoversReturnedWorkspaceChanges(t *testing.T) {
	repo := initProcessTestRepository(t)
	returned := runnerRecoveryBundle(t, "recovered-before-failure.txt", "failed edit")
	engineFailure := errors.New("engine failed after editing")
	processor, _, client := runnerRecoveryProcessor(t, repo, processTestEngine{workspace: repo, fail: engineFailure}, returned)
	safe := processTestSafeContext(repo)
	safe.Runtime = executioncontext.RuntimeContext{}
	run := store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID, Status: "STARTING"}

	result, err := processor.Process(context.Background(), &store.SchedulerAdmission{Run: run, RunnerID: "runner-1"}, processTestLifecycle{run: run})
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != "FAILED" || result.FailureReason == nil || !strings.Contains(*result.FailureReason, engineFailure.Error()) {
		t.Fatalf("result=%+v", result)
	}
	content, err := os.ReadFile(filepath.Join(repo, "recovered-before-failure.txt"))
	if err != nil {
		t.Fatalf("recovered failure edit: %v", err)
	}
	if string(content) != "failed edit" {
		t.Fatalf("recovered failure content=%q", content)
	}
	if len(client.applied) != 1 {
		t.Fatalf("recovered failure acknowledgements=%v", client.applied)
	}
}

func runnerRecoveryBundle(t *testing.T, name, content string) []byte {
	t.Helper()
	runnerRepo := initProcessTestRepository(t)
	if err := os.WriteFile(filepath.Join(runnerRepo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := git.TransferSnapshot(context.Background(), runnerRepo, "runner-recovery")
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func runnerRecoveryProcessor(t *testing.T, repo string, adapter engine.Engine, returned []byte) (*Processor, *runnerSyncStore, *recordingSyncClient) {
	t.Helper()
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
	registry, err := engine.NewRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingSyncClient{receive: func(context.Context) (string, []byte, error) {
		return "recovery-transfer", returned, nil
	}}
	processor, err := NewProcessor(evidenceStore, processTestResolver{resolved: executioncontext.Resolved{Safe: safe}}, nil, &runnerSyncSessions{}, registry, recorder, output, candidate, git, recordingSyncConnector{client: client})
	if err != nil {
		t.Fatal(err)
	}
	return processor, evidenceStore, client
}
