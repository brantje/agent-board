package runexec

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
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
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func TestOpenCodeRunnerNormalCodingRun(t *testing.T) {
	runOpenCodeRunnerNormalCoding(t, false)
}

func TestOpenCodeInternalRunnerNormalCodingRun(t *testing.T) {
	runOpenCodeRunnerNormalCoding(t, true)
}

func runOpenCodeRunnerNormalCoding(t *testing.T, internal bool) {
	if internal && strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_REMOTE_SSH")) != "" {
		t.Skip("remote SSH E2E uses the external runner path")
	}
	env := requireOpenCodeRunnerEnv(t)
	resetRunexecIntegrationDatabase(t, env.databaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	database, err := postgres.Open(ctx, env.databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	cipher, err := secrets.NewAESGCM(1, map[int][]byte{1: bytes.Repeat([]byte{0x5a}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	secretService, err := secrets.NewService(database, cipher)
	if err != nil {
		t.Fatal(err)
	}

	repositoryPath := createOpenCodeRunnerFixtureRepository(t, ctx)
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
	services, err := app.NewServicesWithRuntimes(database, materializer, nil, secretService)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := services.Close(); err != nil {
			t.Errorf("close services: %v", err)
		}
	})

	baseBlobs, err := evidence.NewFileBlobStore(filepath.Join(t.TempDir(), "evidence"), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := evidence.NewRedactingBlobStore(baseBlobs, services.Redaction)
	if err != nil {
		t.Fatal(err)
	}
	services.RunEvidence, err = app.NewRunEvidenceService(services.ExecutionStore, blobs)
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
	engines, err := engine.NewRegistry(opencode.New())
	if err != nil {
		t.Fatal(err)
	}
	runnerConnector := NewRegistryConnector(services.ControlPlane.Runners.Connections)
	processor, err := NewProcessor(services.ExecutionStore, services.ExecutionContext, services.RuntimeInstances, services.ExecutionSessions, engines, recorder, output, candidate, git, runnerConnector)
	if err != nil {
		t.Fatal(err)
	}
	processor.SetWorkspaceEnsurer(services.Workspaces)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runner/ws" {
			http.NotFound(w, r)
			return
		}
		services.ControlPlane.Runners.Connections.ServeHTTP(w, r)
	})
	server := startOpenCodeRunnerWSServer(t, handler)
	runner := admitOpenCodeRunner(t, ctx, services, database, server.URL, runnerWorkspaceRoot, internal)

	marker := uniqueOpenCodeRunnerMarker(t)
	project, run := createOpenCodeRunnerIntegrationRun(t, ctx, services, secretService, env, repositoryPath, marker)

	config := scheduler.DefaultConfig("opencode-runner-integration")
	config.PollInterval = 50 * time.Millisecond
	config.LeaseDuration = 30 * time.Second
	config.HeartbeatInterval = 5 * time.Second
	config.CapacityBackoff = 50 * time.Millisecond
	config.MaxInFlight = 1
	coordinator, err := scheduler.New(services.ExecutionStore, processor, processor, config)
	if err != nil {
		t.Fatal(err)
	}
	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- coordinator.Run(schedulerCtx) }()
	t.Cleanup(func() {
		stopScheduler()
		select {
		case err := <-done:
			if err != nil && err != context.Canceled {
				t.Errorf("scheduler stopped with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("scheduler did not stop")
		}
	})

	terminal := waitForScriptedRun(t, ctx, database, project.ID, run.ID)
	if terminal.Status != "READY_FOR_REVIEW" {
		reason := openCodeFailureReason(terminal.FailureReason)
		events, _ := database.ListRunEvents(context.Background(), project.ID, run.ID, 0, 80)
		t.Fatalf("run status=%s failure=%s events=%v logs=%s", terminal.Status, reason, eventTypes(events), openCodeRunnerFailureLogs(t, services, project.ID, run.ID, env.apiKey))
	}

	workspaceRecord, err := database.GetWorkspace(ctx, project.ID, terminal.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	assertOpenCodeWorkspaceFileEither(t, ctx, database, project.ID, terminal.WorkspaceID, "e2e-openrouter.txt", marker, marker+"\n")
	for _, ignored := range []string{".env", filepath.Join("node_modules", "ignored.js")} {
		if _, err := os.Stat(filepath.Join(workspaceRecord.Path, ignored)); !os.IsNotExist(err) {
			t.Fatalf("ignored path %s unexpectedly synced back from the runner: err=%v", ignored, err)
		}
	}

	sessions, err := database.ListExecutionSessionsByRun(ctx, project.ID, run.ID, nil)
	if err != nil || len(sessions) != 1 || sessions[0].RunnerID != runner.ID || sessions[0].RuntimeInstanceID != "" {
		t.Fatalf("runner sessions=%+v err=%v", sessions, err)
	}
	// OpenCode serve is a long-running process stopped with Terminate after a
	// successful Run. That is recorded as CANCELLED, matching the Docker path.
	if sessions[0].Status != "COMPLETED" && sessions[0].Status != "CANCELLED" {
		t.Fatalf("execution session status=%q", sessions[0].Status)
	}
	if !strings.Contains(string(sessions[0].CommandArgv), "opencode") || !strings.Contains(string(sessions[0].CommandArgv), "serve") {
		t.Fatalf("execution session command=%s", sessions[0].CommandArgv)
	}

	instances, err := database.ListRuntimeInstances(ctx, project.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("runner OpenCode path created Runtime Instances: %+v", instances)
	}

	encoded, err := database.GetRunProvenance(ctx, project.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var provenance executioncontext.Provenance
	if err := json.Unmarshal(encoded, &provenance); err != nil {
		t.Fatal(err)
	}
	if provenance.Context.Runner == nil || provenance.Context.Runner.ID != runner.ID {
		t.Fatalf("runner provenance=%+v want runner %s", provenance.Context.Runner, runner.ID)
	}
	if provenance.Context.Runner.Internal != internal {
		t.Fatalf("runner provenance internal=%v want %v", provenance.Context.Runner.Internal, internal)
	}
	if provenance.Context.Runtime.ID != "" {
		t.Fatalf("runner OpenCode provenance unexpectedly recorded Runtime %q", provenance.Context.Runtime.ID)
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
	completedTransfers := 0
	for _, event := range runEvidence.Events {
		if event.Type == "workspace.transfer.completed" {
			completedTransfers++
		}
	}
	if completedTransfers < 2 {
		t.Fatalf("want to_runner and from_runner transfer completion, completed=%d events=%v", completedTransfers, eventTypes(runEvidence.Events))
	}

	sessionDir := filepath.Join(runnerWorkspaceRoot, sessions[0].ID)
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("runner session workspace %s was not cleaned after sync-back: err=%v", sessionDir, err)
	}

	assertOpenCodeSecretAbsent(t, &openCodeIntegrationFixture{
		ctx: ctx, services: services,
	}, project.ID, run.ID, env.apiKey)
}

type openCodeRunnerEnv struct {
	databaseURL  string
	apiKey       string
	providerKind string
	modelName    string
}

func requireOpenCodeRunnerEnv(t *testing.T) openCodeRunnerEnv {
	t.Helper()
	if os.Getenv("AGENT_BOARD_TEST_OPENCODE") != "1" {
		t.Skip("AGENT_BOARD_TEST_OPENCODE=1 is required for the credential-gated OpenCode runner integration")
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode must be on PATH for the OpenCode runner integration")
	}
	env := openCodeRunnerEnv{
		databaseURL:  strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_DATABASE_URL")),
		apiKey:       firstNonEmptyEnv("AGENT_BOARD_TEST_OPENCODE_API_KEY", "OPENROUTER_API_KEY"),
		providerKind: strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_PROVIDER_KIND")),
		modelName:    strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_MODEL")),
	}
	if env.providerKind == "" {
		env.providerKind = "openrouter"
	}
	if env.modelName == "" {
		env.modelName = firstCSV(os.Getenv("OPENROUTER_MODELS"))
	}
	if env.databaseURL == "" || env.apiKey == "" || env.modelName == "" {
		t.Skip("database URL, OpenCode API key and model are required")
	}
	return env
}

func firstNonEmptyEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func firstCSV(value string) string {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			return part
		}
	}
	return ""
}

func uniqueOpenCodeRunnerMarker(t *testing.T) string {
	t.Helper()
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return "openrouter-runner-" + hex.EncodeToString(raw[:])
}

func createOpenCodeRunnerFixtureRepository(t *testing.T, ctx context.Context) string {
	t.Helper()
	repositoryPath := filepath.Join(t.TempDir(), "fixture")
	if err := os.MkdirAll(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"git", "init", "-q", "-b", "main"}, {"git", "config", "user.email", "integration@example.invalid"}, {"git", "config", "user.name", "Agent Board Integration"}} {
		runIntegrationCommand(t, ctx, repositoryPath, command...)
	}
	if err := os.WriteFile(filepath.Join(repositoryPath, "README.md"), []byte("OpenCode runner fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repositoryPath, ".gitignore"), []byte(".env\nnode_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIntegrationCommand(t, ctx, repositoryPath, "git", "add", ".")
	runIntegrationCommand(t, ctx, repositoryPath, "git", "commit", "-qm", "baseline")
	return repositoryPath
}

func createOpenCodeRunnerIntegrationRun(t *testing.T, ctx context.Context, services *app.Services, secretService *secrets.Service, env openCodeRunnerEnv, repositoryPath, marker string) (store.Project, store.Run) {
	t.Helper()
	project, err := services.ControlPlane.CreateProject(ctx, store.Project{
		Name: "OpenCode runner integration", IssuePrefix: "OCR", RepositoryPath: repositoryPath,
		DefaultBranch: "main", WorkflowSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	credentialRef := "opencode-runner-provider-key"
	if _, err := secretService.Put(ctx, secrets.Scope{ProjectID: &project.ID}, credentialRef, []byte(env.apiKey)); err != nil {
		t.Fatalf("store provider credential: %v", err)
	}
	var baseURL *string
	if value := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_BASE_URL")); value != "" {
		baseURL = &value
	}
	provider, err := services.ControlPlane.CreateProvider(ctx, store.Provider{
		Name: "OpenCode runner provider", Kind: env.providerKind, BaseURL: baseURL,
		CredentialRef: &credentialRef, Enabled: true, HealthStatus: "HEALTHY", SafeMetadata: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := project.ID
	model, err := services.ControlPlane.CreateModelProfile(ctx, store.ModelProfile{
		ProjectID: &scope, ProviderID: provider.ID, Name: "OpenCode runner model", Model: env.modelName,
		GenerationSettings: store.EmptyObject, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := services.ControlPlane.CreateAgent(ctx, store.Agent{
		ProjectID: &scope, Name: "OpenCode runner agent",
		RoleInstructions: "Follow the issue instructions exactly. Do not ask a Question unless the issue explicitly requires human input.",
		Engine:           opencode.Name, ModelProfileID: model.ID,
		EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, State: "ENABLED",
	})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := services.ControlPlane.CreateIssue(ctx, store.Issue{
		ProjectID: project.ID,
		Title:     "Prove the OpenCode runner OpenRouter coding path",
		Description: strings.Join([]string{
			"Do not ask any Question.",
			"Create e2e-openrouter.txt in the project root containing exactly " + marker + " with no extra text.",
			"Also create .env containing ignored-secret-must-not-sync and node_modules/ignored.js containing ignored-module-must-not-sync.",
			"Do not modify README.md.",
			"After writing those files, stop.",
		}, " "),
		Status: "TODO",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := services.ControlPlane.AssignIssue(ctx, project.ID, issue.ID, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	return project, run
}

func startOpenCodeRunnerWSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listen := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_LISTEN"))
	if listen == "" {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		return server
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func admitOpenCodeRunner(t *testing.T, ctx context.Context, services *app.Services, database *postgres.Store, serverURL, workspaceRoot string, internal bool) store.Runner {
	t.Helper()
	binary := buildAgentRunnerBinary(t)
	var admitted store.Runner
	if internal {
		superviseCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			done <- services.ControlPlane.Runners.SuperviseInternalRunner(superviseCtx, binary, serverURL, workspaceRoot)
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("internal runner supervision: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("internal runner supervisor did not stop")
			}
		})
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			runners, err := services.ControlPlane.Runners.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range runners {
				if candidate.Internal && services.ControlPlane.Runners.Connections.Connected(candidate.ID) {
					admitted = candidate
					goto connected
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("internal runner did not connect")
	} else {
		created, token, err := createExternalRunnerCredential(ctx, services.ControlPlane)
		if err != nil {
			t.Fatal(err)
		}
		admitted = created
		if sshTarget := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_REMOTE_SSH")); sshTarget != "" {
			startRemoteOpenCodeAgentRunner(t, sshTarget, binary, serverURL, admitted.ID, token)
		} else {
			proc := startOpenCodeAgentRunner(t, binary, serverURL, admitted.ID, token, workspaceRoot)
			t.Cleanup(func() {
				if proc.Process != nil {
					_ = proc.Process.Kill()
				}
				_ = proc.Wait()
			})
		}
		waitForRunnerConnected(t, services.ControlPlane.Runners.Connections, admitted.ID)
	}
connected:
	database.SetRunnerCandidates(services.ControlPlane.Runners.Connections.Candidates)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, id := range services.ControlPlane.Runners.Connections.Candidates(opencode.Name) {
			if id == admitted.ID {
				return admitted
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("live registry did not advertise the connected runner for the OpenCode engine")
	return admitted
}

func startRemoteOpenCodeAgentRunner(t *testing.T, sshTarget, binary, serverURL, runnerID, token string) {
	t.Helper()
	remotePath := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_REMOTE_PATH"))
	if remotePath == "" {
		t.Fatal("AGENT_BOARD_TEST_REMOTE_PATH is required for remote runner E2E")
	}
	remoteBin := "/tmp/agent-board-e2e-runner"
	copyCmd := exec.Command("scp", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", binary, sshTarget+":"+remoteBin)
	if err := copyCmd.Run(); err != nil {
		t.Fatalf("copy agent-runner to remote host: %v", err)
	}
	remoteHome := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_REMOTE_HOME"))
	if remoteHome == "" {
		t.Fatal("AGENT_BOARD_TEST_REMOTE_HOME is required for remote runner E2E")
	}
	remoteUser := filepath.Base(remoteHome)
	remoteRoot := "/tmp/agent-board-e2e-workspaces"
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", sshTarget,
		"env",
		"PATH="+remotePath,
		"HOME="+remoteHome,
		"USER="+remoteUser,
		"LOGNAME="+remoteUser,
		"LANG=C.UTF-8",
		"TMPDIR=/tmp",
		"AGENT_BOARD_URL="+serverURL,
		"AGENT_RUNNER_ID="+runnerID,
		"AGENT_RUNNER_TOKEN="+token,
		"AGENT_RUNNER_WORKSPACE_ROOT="+remoteRoot,
		remoteBin,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start remote agent-runner: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		_ = exec.Command("ssh", "-o", "BatchMode=yes", sshTarget, "pkill", "-f", remoteBin).Run()
	})
}

func startOpenCodeAgentRunner(t *testing.T, binary, serverURL, runnerID, token, workspaceRoot string) *exec.Cmd {
	t.Helper()
	opencodePath, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(),
		"AGENT_BOARD_URL="+serverURL,
		"AGENT_RUNNER_ID="+runnerID,
		"AGENT_RUNNER_TOKEN="+token,
		"AGENT_RUNNER_WORKSPACE_ROOT="+workspaceRoot,
		"PATH="+filepath.Dir(opencodePath)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func openCodeRunnerFailureLogs(t *testing.T, services *app.Services, projectID, runID, secret string) string {
	t.Helper()
	if services == nil || services.RunEvidence == nil {
		return ""
	}
	snapshot, err := services.RunEvidence.Inspect(context.Background(), projectID, runID)
	if err != nil {
		return err.Error()
	}
	var logs []string
	for _, chunk := range snapshot.RawOutput {
		_, reader, err := services.RunEvidence.OpenRawOutput(context.Background(), projectID, runID, chunk.ID)
		if err != nil {
			logs = append(logs, chunk.ID+": "+err.Error())
			continue
		}
		data, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			logs = append(logs, chunk.ID+": "+readErr.Error())
			continue
		}
		text := string(data)
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
		if len(text) > 4000 {
			text = text[len(text)-4000:]
		}
		logs = append(logs, chunk.Stream+": "+strings.TrimSpace(text))
	}
	return strings.Join(logs, " | ")
}
