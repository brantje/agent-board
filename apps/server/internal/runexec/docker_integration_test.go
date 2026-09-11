package runexec

import (
	"context"
	"io"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestScriptedEngineDockerWalkingSkeleton(t *testing.T) {
	if os.Getenv("AGENT_BOARD_TEST_DOCKER") != "1" {
		t.Skip("AGENT_BOARD_TEST_DOCKER=1 is required for live scripted Engine integration")
	}
	databaseURL := os.Getenv("AGENT_BOARD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AGENT_BOARD_TEST_DATABASE_URL is required for live scripted Engine integration")
	}
	image := os.Getenv("AGENT_BOARD_TEST_SCRIPTED_RUNTIME_IMAGE")
	if image == "" {
		image = "agent-board-scripted-runtime:ci"
	}
	resetRunexecIntegrationDatabase(t, databaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
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

	project, run := createScriptedIntegrationRun(t, ctx, services.ControlPlane, repositoryPath, image)
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
	processor, err := NewProcessor(services.ExecutionStore, services.ExecutionContext, services.RuntimeInstances, services.ExecutionSessions, engines, recorder, output, candidate, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	config := scheduler.DefaultConfig("scripted-integration")
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

	queued := waitForRunnerCapacityDeferral(t, ctx, database, project.ID, run.ID)
	stopScheduler()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("scheduler stopped with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	if queued.Status != "QUEUED" || queued.QueueReason == nil || *queued.QueueReason != "runner_capacity" {
		t.Fatalf("legacy docker Run was admitted without a live runner: status=%s queue=%v", queued.Status, queued.QueueReason)
	}
	instances, err := database.ListRuntimeInstances(ctx, project.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("runtime instances were provisioned without a live runner: %+v", instances)
	}
}

func createScriptedIntegrationRun(t *testing.T, ctx context.Context, control *app.Service, repositoryPath, image string) (store.Project, store.Run) {
	t.Helper()
	project, err := control.CreateProject(ctx, store.Project{Name: "Scripted integration", IssuePrefix: "SCR", RepositoryPath: repositoryPath, DefaultBranch: "main", WorkflowSettings: store.EmptyObject})
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
	_, err = control.CreateRuntime(ctx, store.Runtime{ProjectID: &scope, Name: "Scripted runtime", Kind: "docker", Image: image, NetworkPolicy: "outbound", WorkspacePolicy: "issue", Capabilities: store.EmptyObject, Enabled: true, HealthStatus: "HEALTHY"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := control.CreateAgent(ctx, store.Agent{ProjectID: &scope, Name: "Scripted agent", Engine: scripted.Name, ModelProfileID: model.ID, EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, State: "ENABLED"})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := control.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Exercise scripted engine", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := control.AssignIssue(ctx, project.ID, issue.ID, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	return project, run
}

func createScriptedFixtureRepository(t *testing.T, ctx context.Context) string {
	t.Helper()
	repositoryPath := filepath.Join(t.TempDir(), "fixture")
	if err := os.MkdirAll(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"git", "init", "-q", "-b", "main"}, {"git", "config", "user.email", "integration@example.invalid"}, {"git", "config", "user.name", "Agent Board Integration"}} {
		runIntegrationCommand(t, ctx, repositoryPath, command...)
	}
	for _, name := range []string{"staged.txt", "unstaged.txt", "delete.txt", "rename.txt"} {
		if err := os.WriteFile(filepath.Join(repositoryPath, name), []byte("baseline\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repositoryPath, ".gitignore"), []byte("ignored-scripted.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIntegrationCommand(t, ctx, repositoryPath, "git", "add", ".")
	runIntegrationCommand(t, ctx, repositoryPath, "git", "commit", "-qm", "baseline")
	return repositoryPath
}

func runIntegrationCommand(t *testing.T, ctx context.Context, dir string, command ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v: %s", command, err, output)
	}
}

func resetRunexecIntegrationDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var databaseName string
	if err := pool.QueryRow(context.Background(), `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if databaseName != "agent_board_test" || os.Getenv("AGENT_BOARD_TEST_DATABASE_RESET") != "1" {
		t.Fatalf("refusing destructive integration reset for database %q", databaseName)
	}
	if _, err := pool.Exec(context.Background(), `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "database", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), string(schema)); err != nil {
		t.Fatal(err)
	}
}

func waitForRunnerCapacityDeferral(t *testing.T, ctx context.Context, database *postgres.Store, projectID, runID string) store.Run {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := database.GetRun(ctx, projectID, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != "QUEUED" {
			t.Fatalf("legacy docker Run left QUEUED without a live runner: status=%s", run.Status)
		}
		if run.QueueReason != nil && *run.QueueReason == "runner_capacity" {
			return run
		}
		select {
		case <-ctx.Done():
			reason := ""
			if run.QueueReason != nil {
				reason = *run.QueueReason
			}
			t.Fatalf("timed out waiting for runner_capacity deferral: %v status=%s queue=%s", ctx.Err(), run.Status, reason)
		case <-ticker.C:
		}
	}
}

func waitForScriptedRun(t *testing.T, ctx context.Context, database *postgres.Store, projectID, runID string) store.Run {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := database.GetRun(ctx, projectID, runID)
		if err != nil {
			t.Fatal(err)
		}
		switch run.Status {
		case "READY_FOR_REVIEW", "FAILED", "CANCELLED":
			return run
		}
		select {
		case <-ctx.Done():
			reason := ""
			if run.FailureReason != nil {
				reason = *run.FailureReason
			}
			events, _ := database.ListRunEvents(context.Background(), projectID, runID, 0, 50)
			types := make([]string, 0, len(events))
			for _, event := range events {
				types = append(types, event.Type)
			}
			t.Fatalf("timed out waiting for Run: %v status=%s failure=%s events=%v", ctx.Err(), run.Status, reason, types)
		case <-ticker.C:
		}
	}
}

func findIntegrationArtifact(t *testing.T, artifacts []store.Artifact, kind, name string) store.Artifact {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.Kind == kind && artifact.Name == name {
			return artifact
		}
	}
	t.Fatalf("artifact kind=%s name=%s not found: %+v", kind, name, artifacts)
	return store.Artifact{}
}

func readIntegrationBlob(t *testing.T, ctx context.Context, blobs evidence.BlobStore, ref string) []byte {
	t.Helper()
	reader, err := blobs.Open(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func hasIntegrationEvent(events []store.Event, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func eventTypes(events []store.Event) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}
