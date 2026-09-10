package runexec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

func TestScriptedEngineExternalRunnerWalkingSkeleton(t *testing.T) {
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
	repositoryPolicy, err := repository.NewPolicy([]string{filepath.Dir(repositoryPath)})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "workspaces")
	runnerWorkspaceRoot := filepath.Join(t.TempDir(), "runner-workspaces")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runnerWorkspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	materializer, err := workspace.NewMaterializer(database, repositoryPolicy, git, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	services, err := app.NewServicesWithRuntimes(database, materializer, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := services.Close(); err != nil {
			t.Errorf("close services: %v", err)
		}
	})

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

	binary := buildAgentRunnerBinary(t)
	proc := startExternalAgentRunner(t, binary, server.URL, runner.ID, token, runnerWorkspaceRoot)
	t.Cleanup(func() {
		if proc.Process != nil {
			_ = proc.Process.Kill()
		}
		_ = proc.Wait()
	})
	waitForRunnerConnected(t, services.ControlPlane.Runners.Connections, runner.ID)
	database.SetRunnerCandidates(services.ControlPlane.Runners.Connections.Candidates)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, id := range services.ControlPlane.Runners.Connections.Candidates(scripted.Name) {
			if id == runner.ID {
				goto admitted
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("live registry did not advertise the connected runner for the scripted engine")
admitted:
	project, run := createRunnerScriptedIntegrationRun(t, ctx, services.ControlPlane, repositoryPath)
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
	candidate, err := evidence.NewCandidateSnapshotter(evidence.NewCandidateCollector(), services.ExecutionStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	engines, err := engine.NewRegistry(scripted.New())
	if err != nil {
		t.Fatal(err)
	}
	runnerConnector := NewRegistryConnector(services.ControlPlane.Runners.Connections)
	processor, err := NewProcessor(services.ExecutionStore, services.ExecutionContext, services.RuntimeInstances, services.ExecutionSessions, engines, recorder, output, candidate, git, runnerConnector)
	if err != nil {
		t.Fatal(err)
	}
	processor.SetWorkspaceEnsurer(services.Workspaces)
	config := scheduler.DefaultConfig("runner-integration")
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
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("scheduler stopped with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	if terminal.Status != "READY_FOR_REVIEW" {
		reason := ""
		if terminal.FailureReason != nil {
			reason = *terminal.FailureReason
		}
		events, _ := database.ListRunEvents(context.Background(), project.ID, run.ID, 0, 80)
		t.Fatalf("run status=%s failure=%s events=%v", terminal.Status, reason, eventTypes(events))
	}

	workspaceRecord, err := database.GetWorkspace(ctx, project.ID, terminal.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"staged.txt", "unstaged.txt", "new-scripted.txt", "renamed.txt"} {
		if _, err := os.Stat(filepath.Join(workspaceRecord.Path, path)); err != nil {
			t.Fatalf("workspace change %s missing after runner sync-back: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workspaceRecord.Path, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete.txt still exists after runner execution: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRecord.Path, "rename.txt")); !os.IsNotExist(err) {
		t.Fatalf("rename.txt still exists after runner execution: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRecord.Path, "ignored-scripted.txt")); !os.IsNotExist(err) {
		t.Fatalf("ignored Runner file reached authoritative workspace: %v", err)
	}
	// The scripted Runner deliberately stages staged.txt and git-mv's rename.txt.
	// Sync-back is filesystem-only, so none of that Runner staging may mutate the
	// authoritative server index.
	runIntegrationCommand(t, ctx, workspaceRecord.Path, "git", "diff", "--cached", "--exit-code")

	sessions, err := database.ListExecutionSessionsByRunner(ctx, runner.ID, []string{"COMPLETED", "FAILED"})
	if err != nil || len(sessions) != 1 || sessions[0].RunnerID != runner.ID || sessions[0].RuntimeInstanceID != "" {
		t.Fatalf("runner sessions=%+v err=%v", sessions, err)
	}
	if sessions[0].Status != "COMPLETED" {
		t.Fatalf("execution session status=%q", sessions[0].Status)
	}

	inspection, err := app.NewRunEvidenceService(services.ExecutionStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	runEvidence, err := inspection.Inspect(ctx, project.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasIntegrationEvent(runEvidence.Events, "workspace.transfer.completed") {
		t.Fatalf("missing workspace transfer evidence: %+v", eventTypes(runEvidence.Events))
	}
	if !hasIntegrationEvent(runEvidence.Events, "test.completed") {
		t.Fatalf("missing scripted execution evidence: %+v", eventTypes(runEvidence.Events))
	}
	hasCandidateManifest := false
	for _, artifact := range runEvidence.Artifacts {
		if artifact.Name == "ignored-scripted.txt" {
			t.Fatal("ignored Runner file became a candidate artifact")
		}
		if artifact.Kind == "candidate_manifest" {
			hasCandidateManifest = true
		}
	}
	if !hasCandidateManifest {
		t.Fatalf("missing candidate manifest artifact: %+v", runEvidence.Artifacts)
	}
	expectedFileEvidence := []struct {
		eventType string
		path      string
		oldPath   string
		staged    bool
		unstaged  bool
	}{
		{eventType: "file.modified", path: "staged.txt", unstaged: true},
		{eventType: "file.modified", path: "unstaged.txt", unstaged: true},
		{eventType: "file.created", path: "new-scripted.txt", unstaged: true},
		{eventType: "file.deleted", path: "delete.txt", unstaged: true},
		{eventType: "file.deleted", path: "rename.txt", unstaged: true},
		{eventType: "file.created", path: "renamed.txt", unstaged: true},
	}
	for _, expected := range expectedFileEvidence {
		found := false
		for _, event := range runEvidence.Events {
			if event.Type != expected.eventType {
				continue
			}
			var payload evidence.FilePayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("decode %s payload: %v", event.Type, err)
			}
			if payload.Path != expected.path {
				continue
			}
			found = true
			if payload.OldPath != expected.oldPath || payload.Staged != expected.staged || payload.Unstaged != expected.unstaged {
				t.Fatalf("file evidence %s %s payload=%+v", expected.eventType, expected.path, payload)
			}
			break
		}
		if !found {
			t.Fatalf("missing %s evidence for %s in events=%v", expected.eventType, expected.path, eventTypes(runEvidence.Events))
		}
	}
	for _, event := range runEvidence.Events {
		switch event.Type {
		case "file.created", "file.modified", "file.deleted", "file.renamed":
			var payload evidence.FilePayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("decode %s payload while checking ignored files: %v", event.Type, err)
			}
			if payload.Path == "ignored-scripted.txt" || payload.OldPath == "ignored-scripted.txt" {
				t.Fatal("ignored Runner file appeared in candidate evidence")
			}
		}
	}
}

func createExternalRunnerCredential(ctx context.Context, control *app.Service) (store.Runner, string, error) {
	runner, token, err := control.Runners.Create(ctx, "External integration host")
	if err != nil {
		return store.Runner{}, "", err
	}
	return runner, token, nil
}

func createRunnerScriptedIntegrationRun(t *testing.T, ctx context.Context, control *app.Service, repositoryPath string) (store.Project, store.Run) {
	t.Helper()
	project, err := control.CreateProject(ctx, store.Project{Name: "Runner integration", IssuePrefix: "RUN", RepositoryPath: repositoryPath, DefaultBranch: "main", WorkflowSettings: store.EmptyObject})
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
	agent, err := control.CreateAgent(ctx, store.Agent{ProjectID: &scope, Name: "Runner scripted agent", Engine: scripted.Name, ModelProfileID: model.ID, EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, State: "ENABLED"})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := control.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Exercise external runner", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := control.AssignIssue(ctx, project.ID, issue.ID, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	return project, run
}

func buildAgentRunnerBinary(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "agent-runner")
	cmd := exec.Command("go", "build", "-o", binary, "./apps/agent-runner/cmd/agent-runner")
	cmd.Dir = repoRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build agent-runner: %v: %s", err, output)
	}
	return binary
}

func startExternalAgentRunner(t *testing.T, binary, serverURL, runnerID, token, workspaceRoot string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(),
		"AGENT_BOARD_URL="+serverURL,
		"AGENT_RUNNER_ID="+runnerID,
		"AGENT_RUNNER_TOKEN="+token,
		"AGENT_RUNNER_WORKSPACE_ROOT="+workspaceRoot,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func waitForRunnerConnected(t *testing.T, registry interface{ Connected(string) bool }, runnerID string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if registry.Connected(runnerID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for external runner connection")
}
