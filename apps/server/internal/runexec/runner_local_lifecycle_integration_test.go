package runexec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/scripted"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func TestLocalSourceExternalRunnerReviewLifecycle(t *testing.T) {
	if os.Getenv("AGENT_BOARD_TEST_RUNNER") != "1" {
		t.Skip("AGENT_BOARD_TEST_RUNNER=1 is required for live external runner integration")
	}
	databaseURL := os.Getenv("AGENT_BOARD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AGENT_BOARD_TEST_DATABASE_URL is required for live external runner integration")
	}
	resetRunexecIntegrationDatabase(t, databaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	repositoryPath := createScriptedFixtureRepository(t, ctx)
	policy, err := repository.NewPolicy([]string{filepath.Dir(repositoryPath)})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "workspaces")
	runnerRoot := filepath.Join(t.TempDir(), "runner-workspaces")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runnerRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	provisioner, err := repository.NewProvisioner(policy, git)
	if err != nil {
		t.Fatal(err)
	}
	projectMaterializer, err := workspace.NewProjectMaterializer(database, provisioner, git, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	issueMaterializer, err := workspace.NewMaterializer(database, policy, git, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	materializer, err := workspace.NewProjectBackedMaterializer(issueMaterializer, projectMaterializer)
	if err != nil {
		t.Fatal(err)
	}
	services, err := app.NewExecutionServices(database, materializer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = services.Close() })

	runner, token, err := createExternalRunnerCredential(ctx, services.ControlPlane)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runner/ws" {
			http.NotFound(w, r)
			return
		}
		services.ControlPlane.Runners.Connections.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	proc := startExternalAgentRunner(t, buildAgentRunnerBinary(t), server.URL, runner.ID, token, runnerRoot)
	t.Cleanup(func() {
		if proc.Process != nil {
			_ = proc.Process.Kill()
		}
		_ = proc.Wait()
	})
	waitForRunnerConnected(t, services.ControlPlane.Runners.Connections, runner.ID)
	database.SetRunnerCandidates(services.ControlPlane.Runners.Connections.Candidates)

	project, run := createRunnerScriptedIntegrationRun(t, ctx, services.ControlPlane, repositoryPath)
	acceptedBefore, err := projectMaterializer.EnsureProjectWorkspace(ctx, project)
	if err != nil {
		t.Fatal(err)
	}

	baseBlobs, err := evidence.NewFileBlobStore(filepath.Join(t.TempDir(), "evidence"), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := evidence.NewRedactingBlobStore(baseBlobs, services.Redaction)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(services.ExecutionStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(services.ExecutionStore, blobs, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	engines, err := engine.NewRegistry(scripted.New())
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewProcessor(services.ExecutionStore, services.ExecutionContext, services.ExecutionSessions, engines, recorder, output, git, NewRegistryConnector(services.ControlPlane.Runners.Connections))
	if err != nil {
		t.Fatal(err)
	}
	processor.SetWorkspaceEnsurer(services.Workspaces)
	config := scheduler.DefaultConfig("local-lifecycle")
	config.PollInterval = 20 * time.Millisecond
	config.LeaseDuration = 3 * time.Second
	config.HeartbeatInterval = 500 * time.Millisecond
	config.CapacityBackoff = 20 * time.Millisecond
	config.MaxInFlight = 1
	coordinator, err := scheduler.New(services.ExecutionStore, processor, processor, config)
	if err != nil {
		t.Fatal(err)
	}
	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- coordinator.Run(schedulerCtx) }()
	terminal := waitForScriptedRun(t, ctx, database, project.ID, run.ID)
	stopScheduler()
	<-done
	if terminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("run status=%s failure=%v", terminal.Status, terminal.FailureReason)
	}

	workspaceRecord, err := database.GetWorkspace(ctx, project.ID, terminal.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if workspaceRecord.BaseRevision == nil || *workspaceRecord.BaseRevision != acceptedBefore.AcceptedRevision {
		t.Fatalf("Workspace base=%v want target=%q", workspaceRecord.BaseRevision, acceptedBefore.AcceptedRevision)
	}
	reviewRows, err := database.ListReviews(ctx, project.ID, store.ReviewFilter{IssueID: &run.IssueID})
	if err != nil || len(reviewRows) != 1 {
		t.Fatalf("reviews=%+v err=%v", reviewRows, err)
	}
	review := reviewRows[0]
	if review.BaseRevision != acceptedBefore.AcceptedRevision || review.ReviewRevision == "" || review.ReviewRevision == review.BaseRevision {
		t.Fatalf("Review Git identity=%+v target=%q", review, acceptedBefore.AcceptedRevision)
	}
	parent := integrationGitOutput(t, ctx, workspaceRecord.Path, "rev-parse", review.ReviewRevision+"^")
	if parent != review.BaseRevision {
		t.Fatalf("Runner branch started from %q want %q", parent, review.BaseRevision)
	}
	diff := integrationGitOutput(t, ctx, workspaceRecord.Path, "diff", "--name-status", review.BaseRevision+".."+review.ReviewRevision)
	if !strings.Contains(diff, "new-scripted.txt") || !strings.Contains(diff, "staged.txt") {
		t.Fatalf("review diff did not reproduce Runner changes:\n%s", diff)
	}

	runEvidence, err := app.NewRunEvidenceService(services.ExecutionStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := app.NewReviewService(database, database, runEvidence, services.Workspaces)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := reviews.Get(ctx, project.ID, review.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Review.BaseRevision != review.BaseRevision || inspection.Review.ReviewRevision != review.ReviewRevision {
		t.Fatalf("inspection changed Review identity: %+v", inspection.Review)
	}

	// Advance the mutable Issue branch after Review creation. Approval must use
	// the immutable Review SHA and must not deliver this unreviewed commit.
	if err := os.WriteFile(filepath.Join(workspaceRecord.Path, "unreviewed.txt"), []byte("not reviewed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIntegrationCommand(t, ctx, workspaceRecord.Path, "git", "add", "unreviewed.txt")
	runIntegrationCommand(t, ctx, workspaceRecord.Path, "git", "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "unreviewed follow-up")
	mutableHead := integrationGitOutput(t, ctx, workspaceRecord.Path, "rev-parse", "HEAD")
	if mutableHead == review.ReviewRevision {
		t.Fatal("mutable Issue branch did not advance after Review pinning")
	}

	approved, err := reviews.Approve(ctx, project.ID, review.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Review.Status != "APPROVED" || approved.Run.Status != "COMPLETED" || approved.Issue.Status != "DONE" {
		t.Fatalf("approval result=%+v", approved)
	}
	acceptedAfter, err := projectMaterializer.EnsureProjectWorkspace(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(acceptedAfter.Path, "new-scripted.txt")); err != nil {
		t.Fatalf("reviewed Runner change missing from Project target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(acceptedAfter.Path, "unreviewed.txt")); !os.IsNotExist(err) {
		t.Fatalf("unreviewed mutable Workspace state was delivered: %v", err)
	}
	if _, err := exec.CommandContext(ctx, "git", "-C", acceptedAfter.Path, "merge-base", "--is-ancestor", review.ReviewRevision, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("Project target does not contain pinned Review revision: %v", err)
	}
	persistedRun, err := database.GetRun(ctx, project.ID, run.ID)
	if err != nil || persistedRun.Status != "COMPLETED" {
		t.Fatalf("persisted Run=%+v err=%v", persistedRun, err)
	}
	persistedIssue, err := database.GetIssue(ctx, project.ID, run.IssueID)
	if err != nil || persistedIssue.Status != "DONE" {
		t.Fatalf("persisted Issue=%+v err=%v", persistedIssue, err)
	}
}

func integrationGitOutput(t *testing.T, ctx context.Context, repositoryPath string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repositoryPath}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
