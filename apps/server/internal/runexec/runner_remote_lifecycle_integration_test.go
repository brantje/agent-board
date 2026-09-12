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

func TestRemoteGitExternalRunnerLifecycleContinuesAcrossRunners(t *testing.T) {
	if os.Getenv("AGENT_BOARD_TEST_RUNNER") != "1" {
		t.Skip("AGENT_BOARD_TEST_RUNNER=1 is required for live external runner integration")
	}
	databaseURL := os.Getenv("AGENT_BOARD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AGENT_BOARD_TEST_DATABASE_URL is required for live external runner integration")
	}
	resetRunexecIntegrationDatabase(t, databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	origin, targetRevision := createRemoteLifecycleOrigin(t, ctx)
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	policy, err := repository.NewPolicy([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	materializer, err := workspace.NewMaterializer(database, policy, git, filepath.Join(root, "server-workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	services, err := app.NewExecutionServices(database, materializer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = services.Close() })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runner/ws" {
			http.NotFound(w, r)
			return
		}
		services.ControlPlane.Runners.Connections.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	binary := buildAgentRunnerBinary(t)

	runner1, token1, err := createExternalRunnerCredential(ctx, services.ControlPlane)
	if err != nil {
		t.Fatal(err)
	}
	proc1 := startExternalAgentRunner(t, binary, server.URL, runner1.ID, token1, filepath.Join(root, "runner-1"))
	waitForRunnerConnected(t, services.ControlPlane.Runners.Connections, runner1.ID)
	database.SetRunnerCandidates(services.ControlPlane.Runners.Connections.Candidates)

	project, run1 := createRemoteScriptedIntegrationRun(t, ctx, services.ControlPlane, origin)
	workspaceRecord, err := database.GetWorkspace(ctx, project.ID, run1.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if workspaceRecord.BootstrapStatus != "PENDING" || workspaceRecord.BaseRevision != nil {
		t.Fatalf("new remote Workspace=%+v", workspaceRecord)
	}

	baseBlobs, err := evidence.NewFileBlobStore(filepath.Join(root, "evidence"), 8<<20)
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

	runSchedulerUntilTerminal(t, ctx, services.ExecutionStore, processor, database, project.ID, run1.ID, "remote-lifecycle-1")
	persisted1, err := database.GetRun(ctx, project.ID, run1.ID)
	if err != nil || persisted1.Status != "READY_FOR_REVIEW" {
		t.Fatalf("first Run=%+v err=%v", persisted1, err)
	}
	workspaceRecord, err = database.GetWorkspace(ctx, project.ID, workspaceRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if workspaceRecord.BootstrapStatus != "READY" || workspaceRecord.BaseRevision == nil || *workspaceRecord.BaseRevision != targetRevision {
		t.Fatalf("remote Workspace bootstrap=%+v target=%q", workspaceRecord, targetRevision)
	}
	revision1, err := database.GetWorkspaceCurrentRevision(ctx, project.ID, workspaceRecord.ID)
	if err != nil || revision1 == "" || revision1 == targetRevision {
		t.Fatalf("first published revision=%q err=%v target=%q", revision1, err, targetRevision)
	}
	remoteBranch := "refs/heads/" + workspaceRecord.WorkingBranch
	if got := integrationGitOutput(t, ctx, origin, "rev-parse", remoteBranch); got != revision1 {
		t.Fatalf("origin Issue branch=%q want persisted=%q", got, revision1)
	}
	if parent := integrationGitOutput(t, ctx, origin, "rev-parse", revision1+"^"); parent != targetRevision {
		t.Fatalf("Runner 1 started from %q want configured target %q", parent, targetRevision)
	}
	if got := integrationGitOutput(t, ctx, origin, "rev-parse", "refs/heads/main"); got != targetRevision {
		t.Fatalf("Issue publication moved remote target: got=%q want=%q", got, targetRevision)
	}

	runEvidence, err := app.NewRunEvidenceService(services.ExecutionStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := app.NewReviewService(database, database, runEvidence, services.Workspaces)
	if err != nil {
		t.Fatal(err)
	}
	review1 := reviewForRun(t, ctx, database, project.ID, run1.ID)
	if review1.BaseRevision != targetRevision || review1.ReviewRevision != revision1 {
		t.Fatalf("first Review=%+v target=%q published=%q", review1, targetRevision, revision1)
	}
	changes, err := reviews.RequestChanges(ctx, project.ID, review1.ID, "continue on another runner", nil)
	if err != nil {
		t.Fatal(err)
	}
	run2 := changes.Run
	if run2.Attempt != run1.Attempt+1 || run2.WorkspaceID != run1.WorkspaceID {
		t.Fatalf("second Run=%+v first=%+v", run2, run1)
	}

	if proc1.Process != nil {
		_ = proc1.Process.Kill()
	}
	_ = proc1.Wait()
	waitForRunnerDisconnected(t, services.ControlPlane.Runners.Connections, runner1.ID)

	runner2, token2, err := createExternalRunnerCredential(ctx, services.ControlPlane)
	if err != nil {
		t.Fatal(err)
	}
	proc2 := startExternalAgentRunner(t, binary, server.URL, runner2.ID, token2, filepath.Join(root, "runner-2"))
	t.Cleanup(func() {
		if proc2.Process != nil {
			_ = proc2.Process.Kill()
		}
		_ = proc2.Wait()
	})
	waitForRunnerConnected(t, services.ControlPlane.Runners.Connections, runner2.ID)

	runSchedulerUntilTerminal(t, ctx, services.ExecutionStore, processor, database, project.ID, run2.ID, "remote-lifecycle-2")
	persisted2, err := database.GetRun(ctx, project.ID, run2.ID)
	if err != nil || persisted2.Status != "READY_FOR_REVIEW" {
		t.Fatalf("second Run=%+v err=%v", persisted2, err)
	}
	revision2, err := database.GetWorkspaceCurrentRevision(ctx, project.ID, workspaceRecord.ID)
	if err != nil || revision2 == "" || revision2 == revision1 {
		t.Fatalf("second published revision=%q err=%v first=%q", revision2, err, revision1)
	}
	if parent := integrationGitOutput(t, ctx, origin, "rev-parse", revision2+"^"); parent != revision1 {
		t.Fatalf("Runner 2 started from %q want persisted Issue revision %q", parent, revision1)
	}
	if got := integrationGitOutput(t, ctx, origin, "rev-parse", remoteBranch); got != revision2 {
		t.Fatalf("final origin Issue branch=%q want=%q", got, revision2)
	}
	if got := integrationGitOutput(t, ctx, origin, "rev-parse", "refs/heads/main"); got != targetRevision {
		t.Fatalf("second Issue publication moved remote target: got=%q want=%q", got, targetRevision)
	}

	review2 := reviewForRun(t, ctx, database, project.ID, run2.ID)
	if review2.BaseRevision != targetRevision || review2.ReviewRevision != revision2 {
		t.Fatalf("final Review=%+v base=%q head=%q", review2, targetRevision, revision2)
	}
	diff := integrationGitOutput(t, ctx, origin, "diff", "--name-status", review2.BaseRevision+".."+review2.ReviewRevision)
	if !strings.Contains(diff, "new-scripted.txt") || !strings.Contains(diff, "staged.txt") {
		t.Fatalf("final Review diff is not reproducible:\n%s", diff)
	}
	issueBeforeApproval, err := database.GetIssue(ctx, project.ID, run2.IssueID)
	if err != nil || issueBeforeApproval.Status != "REVIEW" {
		t.Fatalf("remote Issue before approval=%+v err=%v", issueBeforeApproval, err)
	}
	approved, err := reviews.Approve(ctx, project.ID, review2.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Review.Status != "APPROVED" || approved.Run.Status != "READY_FOR_REVIEW" || approved.Issue.Status != "REVIEW" {
		t.Fatalf("remote approval incorrectly claimed target delivery: %+v", approved)
	}
	if got := integrationGitOutput(t, ctx, origin, "rev-parse", "refs/heads/main"); got != targetRevision {
		t.Fatalf("remote Review approval moved target without provider integration: got=%q want=%q", got, targetRevision)
	}
}

func createRemoteLifecycleOrigin(t *testing.T, ctx context.Context) (string, string) {
	t.Helper()
	working := createScriptedFixtureRepository(t, ctx)
	origin := filepath.Join(t.TempDir(), "origin.git")
	command := exec.CommandContext(ctx, "git", "clone", "--bare", working, origin)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create bare origin: %v: %s", err, output)
	}
	return origin, integrationGitOutput(t, ctx, origin, "rev-parse", "refs/heads/main")
}

func createRemoteScriptedIntegrationRun(t *testing.T, ctx context.Context, control *app.Service, origin string) (store.Project, store.Run) {
	t.Helper()
	ref := "main"
	project, err := control.CreateProject(ctx, store.Project{Name: "Remote Runner integration", IssuePrefix: "RMT", SourceType: store.ProjectSourceGit, CloneURL: &origin, SourceRef: &ref, WorkflowSettings: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := control.CreateProvider(ctx, store.Provider{Name: "Fixture", Kind: "fixture", Enabled: true, HealthStatus: "HEALTHY", SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	scope := project.ID
	model, err := control.CreateModelProfile(ctx, store.ModelProfile{ProjectID: &scope, ProviderID: provider.ID, Name: "Fixture model", Model: "fixture", GenerationSettings: store.EmptyObject, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := control.CreateAgent(ctx, store.Agent{ProjectID: &scope, Name: "Remote scripted agent", Engine: scripted.Name, ModelProfileID: model.ID, EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, State: "ENABLED"})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := control.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Exercise remote runner", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := control.AssignIssue(ctx, project.ID, issue.ID, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	return project, run
}

func runSchedulerUntilTerminal(t *testing.T, ctx context.Context, executionStore store.SchedulerStore, processor *Processor, database *postgres.Store, projectID, runID, owner string) {
	t.Helper()
	config := scheduler.DefaultConfig(owner)
	config.PollInterval = 20 * time.Millisecond
	config.LeaseDuration = 3 * time.Second
	config.HeartbeatInterval = 500 * time.Millisecond
	config.CapacityBackoff = 20 * time.Millisecond
	config.MaxInFlight = 1
	coordinator, err := scheduler.New(executionStore, processor, processor, config)
	if err != nil {
		t.Fatal(err)
	}
	schedulerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- coordinator.Run(schedulerCtx) }()
	_ = waitForScriptedRun(t, ctx, database, projectID, runID)
	stop()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("scheduler stopped with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func reviewForRun(t *testing.T, ctx context.Context, database *postgres.Store, projectID, runID string) store.Review {
	t.Helper()
	reviews, err := database.ListReviews(ctx, projectID, store.ReviewFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, review := range reviews {
		if review.RunID == runID {
			return review
		}
	}
	t.Fatalf("Review for Run %s was not created", runID)
	return store.Review{}
}

func waitForRunnerDisconnected(t *testing.T, registry interface{ Connected(string) bool }, runnerID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !registry.Connected(runnerID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Runner %s to disconnect", runnerID)
}
