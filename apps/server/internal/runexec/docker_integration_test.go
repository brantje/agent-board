package runexec

import (
	"context"
	"encoding/json"
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
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	dockerruntime "github.com/brantje/agent-board/apps/server/internal/runtime/docker"
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	repositoryPath := createScriptedFixtureRepository(t)
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
	dockerRuntime, err := dockerruntime.New()
	if err != nil {
		t.Fatal(err)
	}
	services, err := app.NewServicesWithRuntimes(database, materializer, map[string]runtimepkg.Implementation{"docker": dockerRuntime})
	if err != nil {
		t.Fatal(err)
	}
	defer services.Close()

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
	processor, err := NewProcessor(services.ExecutionStore, services.ExecutionContext, services.RuntimeInstances, services.ExecutionSessions, engines, recorder, output, candidate)
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
		t.Fatalf("run status=%s failure=%v", terminal.Status, terminal.FailureReason)
	}

	workspaceRecord, err := database.GetWorkspace(ctx, project.ID, terminal.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"staged.txt", "unstaged.txt", "new-scripted.txt", "renamed.txt"} {
		if _, err := os.Stat(filepath.Join(workspaceRecord.Path, path)); err != nil {
			t.Fatalf("workspace change %s did not survive Runtime destruction: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workspaceRecord.Path, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete.txt still exists after scripted execution: %v", err)
	}

	instances, err := database.ListRuntimeInstances(ctx, project.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].Status != string(runtimepkg.StateDestroyed) {
		t.Fatalf("runtime instances=%+v", instances)
	}

	inspection, err := app.NewRunEvidenceService(services.ExecutionStore, blobs)
	if err != nil {
		t.Fatal(err)
	}
	runEvidence, err := inspection.Inspect(ctx, project.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runEvidence.Provenance) == 0 || len(runEvidence.Sessions) < 8 || len(runEvidence.RawOutput) < 3 || len(runEvidence.Artifacts) == 0 {
		t.Fatalf("incomplete run evidence: sessions=%d raw=%d artifacts=%d provenance=%d", len(runEvidence.Sessions), len(runEvidence.RawOutput), len(runEvidence.Artifacts), len(runEvidence.Provenance))
	}
	if !hasIntegrationEvent(runEvidence.Events, "test.completed") || !hasIntegrationEvent(runEvidence.Events, "file.renamed") || !hasIntegrationEvent(runEvidence.Events, "file.deleted") || !hasIntegrationEvent(runEvidence.Events, "file.created") {
		t.Fatalf("missing normalized execution evidence: %+v", eventTypes(runEvidence.Events))
	}
	for _, event := range runEvidence.Events {
		if len(event.Payload) > 8<<10 {
			t.Fatalf("large process output was duplicated into Event %s (%d bytes)", event.Type, len(event.Payload))
		}
	}
	for _, chunk := range runEvidence.RawOutput {
		if chunk.SizeBytes > 64<<10 {
			t.Fatalf("raw output chunk exceeded bound: %+v", chunk)
		}
	}

	manifest := findIntegrationArtifact(t, runEvidence.Artifacts, "candidate_manifest", "candidate-manifest.json")
	manifestReader, err := blobs.Open(ctx, manifest.StorageRef)
	if err != nil {
		t.Fatal(err)
	}
	var captured evidence.Candidate
	if err := json.NewDecoder(manifestReader).Decode(&captured); err != nil {
		_ = manifestReader.Close()
		t.Fatal(err)
	}
	_ = manifestReader.Close()
	assertCandidateStatuses(t, captured)

	newFileArtifact := findIntegrationArtifact(t, runEvidence.Artifacts, "candidate_file", "new-scripted.txt")
	before := readIntegrationBlob(t, ctx, blobs, newFileArtifact.StorageRef)
	if err := os.WriteFile(filepath.Join(workspaceRecord.Path, "new-scripted.txt"), []byte("later mutation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := readIntegrationBlob(t, ctx, blobs, newFileArtifact.StorageRef)
	if string(before) != "scripted new\n" || string(after) != string(before) {
		t.Fatalf("prior attempt artifact changed: before=%q after=%q", before, after)
	}
}

func createScriptedIntegrationRun(t *testing.T, ctx context.Context, control *app.Service, repositoryPath, image string) (store.Project, store.Run) {
	t.Helper()
	project, err := control.CreateProject(ctx, store.Project{Name: "Scripted integration", RepositoryPath: repositoryPath, DefaultBranch: "main", WorkflowSettings: store.EmptyObject})
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
	runtimeConfig, err := control.CreateRuntime(ctx, store.Runtime{ProjectID: &scope, Name: "Scripted runtime", Kind: "docker", Image: image, NetworkPolicy: "outbound", WorkspacePolicy: "issue", Capabilities: store.EmptyObject, Enabled: true, HealthStatus: "HEALTHY"})
	if err != nil {
		t.Fatal(err)
	}
	executorProfile, err := control.CreateExecutorProfile(ctx, store.ExecutorProfile{ProjectID: &scope, Name: "Scripted", Engine: scripted.Name, ModelProfileID: model.ID, RuntimeID: runtimeConfig.ID, EngineSettings: store.EmptyObject, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := control.CreateAgent(ctx, store.Agent{ProjectID: &scope, Name: "Scripted agent", ExecutorProfileID: executorProfile.ID, ConcurrencyLimit: 1, State: "ENABLED"})
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

func createScriptedFixtureRepository(t *testing.T) string {
	t.Helper()
	repositoryPath := filepath.Join(t.TempDir(), "fixture")
	if err := os.MkdirAll(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"git", "init", "-q", "-b", "main"}, {"git", "config", "user.email", "integration@example.invalid"}, {"git", "config", "user.name", "Agent Board Integration"}} {
		runIntegrationCommand(t, repositoryPath, command...)
	}
	for _, name := range []string{"staged.txt", "unstaged.txt", "delete.txt", "rename.txt"} {
		if err := os.WriteFile(filepath.Join(repositoryPath, name), []byte("baseline\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runIntegrationCommand(t, repositoryPath, "git", "add", ".")
	runIntegrationCommand(t, repositoryPath, "git", "commit", "-qm", "baseline")
	return repositoryPath
}

func runIntegrationCommand(t *testing.T, dir string, command ...string) {
	t.Helper()
	cmd := exec.Command(command[0], command[1:]...)
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
			t.Fatalf("timed out waiting for Run: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertCandidateStatuses(t *testing.T, candidate evidence.Candidate) {
	t.Helper()
	var staged, unstaged, untracked, deleted, renamed bool
	for _, change := range candidate.Changes {
		staged = staged || change.StagedStatus != ""
		unstaged = unstaged || change.UnstagedStatus != ""
		untracked = untracked || change.Untracked
		deleted = deleted || change.StagedStatus == "deleted" || change.UnstagedStatus == "deleted"
		renamed = renamed || change.StagedStatus == "renamed" || change.UnstagedStatus == "renamed"
	}
	if !staged || !unstaged || !untracked || !deleted || !renamed {
		t.Fatalf("candidate statuses staged=%v unstaged=%v untracked=%v deleted=%v renamed=%v: %+v", staged, unstaged, untracked, deleted, renamed, candidate.Changes)
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
