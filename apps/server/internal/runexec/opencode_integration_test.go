package runexec

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

const invalidOpenRouterIntegrationKey = "agent-board-invalid-openrouter-key-DO-NOT-LEAK-7d39d483b53e4cba"

type openCodeIntegrationEnv struct {
	databaseURL  string
	apiKey       string
	providerKind string
	modelName    string
	image        string
}

type openCodeIntegrationFixture struct {
	ctx            context.Context
	database       *postgres.Store
	services       *app.Services
	secretService  *secrets.Service
	repositoryPath string
	env            openCodeIntegrationEnv
	coordinator    *scheduler.Coordinator
	started        bool
}

type openCodeRunSpec struct {
	apiKey           string
	roleInstructions string
	title            string
	description      string
}

func TestOpenCodeDockerNormalCodingRun(t *testing.T) {
	fixture := newOpenCodeIntegrationFixture(t)
	project, run := fixture.createRun(t, openCodeRunSpec{
		roleInstructions: "Follow the issue instructions exactly. Do not ask a Question unless the issue explicitly requires human input.",
		title:            "Prove the normal OpenCode coding path",
		description:      "Do not ask any Question. Create opencode-result.txt containing exactly normal-run-ok with no trailing newline. Do not modify any other file. After writing that file, stop.",
	})
	fixture.startScheduler(t)

	terminal := waitForScriptedRun(t, fixture.ctx, fixture.database, project.ID, run.ID)
	if terminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("run status=%s failure=%q", terminal.Status, openCodeFailureReason(terminal.FailureReason))
	}
	assertOpenCodeWorkspaceFile(t, fixture.ctx, fixture.database, project.ID, terminal.WorkspaceID, "opencode-result.txt", "normal-run-ok")

	filterRunID := run.ID
	questions, err := fixture.database.ListQuestions(fixture.ctx, project.ID, store.QuestionFilter{RunID: &filterRunID})
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 0 {
		t.Fatalf("normal OpenCode run unexpectedly created Questions: %+v", questions)
	}
	assertOpenCodeServerSession(t, fixture.ctx, fixture.database, project.ID, run.ID, false)
}

func TestOpenCodeDockerInteractiveQuestionRoundTrip(t *testing.T) {
	fixture := newOpenCodeIntegrationFixture(t)
	project, run := fixture.createRun(t, openCodeRunSpec{
		roleInstructions: "Follow the issue instructions exactly. Use OpenCode's native Question tool for the requested human choice and wait for the answer before editing.",
		title:            "Prove the native OpenCode Question round trip",
		description:      "Before changing any files, use OpenCode's native Question tool to ask exactly one blocking single-choice Question: 'Which marker should I write?' with options 'alpha' and 'beta'. Do not use a free-form answer. After the human answer, create opencode-result.txt containing only the selected marker; a single trailing newline is allowed. Do not ask any other Question and do not modify other files.",
	})
	fixture.startScheduler(t)

	t.Log("waiting for native OpenCode Question")
	question := waitForOpenCodeQuestion(t, fixture.ctx, fixture.database, project.ID, run.ID)
	if question.Kind != "SINGLE_CHOICE" {
		t.Fatalf("native OpenCode Question kind=%q want SINGLE_CHOICE", question.Kind)
	}
	betaOption := optionIDByLabel(t, question, "beta")
	if _, err := fixture.services.Questions.Answer(fixture.ctx, project.ID, question.ID, store.QuestionAnswer{
		Kind: "SINGLE_CHOICE", OptionIDs: []string{betaOption},
	}, nil); err != nil {
		t.Fatalf("answer OpenCode Question through Agent Board: %v", err)
	}

	terminal := waitForScriptedRun(t, fixture.ctx, fixture.database, project.ID, run.ID)
	if terminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("run status=%s failure=%q", terminal.Status, openCodeFailureReason(terminal.FailureReason))
	}
	assertOpenCodeWorkspaceFileEither(t, fixture.ctx, fixture.database, project.ID, terminal.WorkspaceID, "opencode-result.txt", "beta", "beta\n")

	events, err := fixture.database.ListRunEvents(fixture.ctx, project.ID, run.ID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	assertOpenCodeQuestionEventOrder(t, events)
	for _, eventType := range []string{"question.created", "run.waiting_for_input", "question.answered", "run.resumed"} {
		assertOpenCodeEventCount(t, events, eventType, 1)
	}
	assertOpenCodeServerSession(t, fixture.ctx, fixture.database, project.ID, run.ID, false)
}

func TestOpenCodeDockerInvalidCredentialsFailWithoutSecretLeak(t *testing.T) {
	fixture := newOpenCodeIntegrationFixture(t)
	project, run := fixture.createRun(t, openCodeRunSpec{
		apiKey:           invalidOpenRouterIntegrationKey,
		roleInstructions: "Follow the issue instructions exactly. Do not ask a Question.",
		title:            "Prove OpenRouter authentication failure handling",
		description:      "Respond to this task using the configured model. Do not ask a Question. If model access succeeds, create should-not-exist.txt containing unexpected-success, then stop.",
	})
	fixture.startScheduler(t)

	terminal := waitForScriptedRun(t, fixture.ctx, fixture.database, project.ID, run.ID)
	if terminal.Status != "FAILED" {
		t.Fatalf("invalid credential run status=%s failure=%q; want FAILED", terminal.Status, openCodeFailureReason(terminal.FailureReason))
	}
	if terminal.FailureReason == nil || strings.TrimSpace(*terminal.FailureReason) == "" {
		t.Fatal("invalid credential failure did not surface through Run failure_reason")
	}
	events, err := fixture.database.ListRunEvents(fixture.ctx, project.ID, run.ID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if openCodeEventIndex(events, "run.failed") < 0 {
		t.Fatalf("invalid credential failure did not persist run.failed: %v", eventTypes(events))
	}
	assertOpenCodeSecretAbsent(t, fixture, project.ID, run.ID, invalidOpenRouterIntegrationKey)
}

func TestOpenCodeDockerCancelWhileWaitingForQuestion(t *testing.T) {
	fixture := newOpenCodeIntegrationFixture(t)
	project, run := fixture.createRun(t, openCodeRunSpec{
		roleInstructions: "Follow the issue instructions exactly. Use OpenCode's native Question tool for the requested human choice and wait for the answer before editing.",
		title:            "Cancel a native OpenCode Question",
		description:      "Before changing any files, use OpenCode's native Question tool to ask exactly one blocking single-choice Question: 'Which marker should I write?' with options 'alpha' and 'beta'. Wait for the human answer. Only after an answer, create opencode-result.txt containing the selected marker. Do not ask another Question or modify another file.",
	})
	fixture.startScheduler(t)

	question := waitForOpenCodeQuestion(t, fixture.ctx, fixture.database, project.ID, run.ID)
	if err := fixture.services.CancelRun(fixture.ctx, project.ID, run.ID); err != nil {
		t.Fatalf("cancel waiting OpenCode Run: %v", err)
	}
	terminal := waitForScriptedRun(t, fixture.ctx, fixture.database, project.ID, run.ID)
	if terminal.Status != "CANCELLED" {
		t.Fatalf("cancelled Run status=%s failure=%q", terminal.Status, openCodeFailureReason(terminal.FailureReason))
	}

	session := waitForOpenCodeSessionStatus(t, fixture.ctx, fixture.database, project.ID, run.ID, "CANCELLED")
	if !strings.Contains(string(session.CommandArgv), "opencode") || !strings.Contains(string(session.CommandArgv), "serve") {
		t.Fatalf("cancelled execution session command=%s", session.CommandArgv)
	}
	workspaceRecord, err := fixture.database.GetWorkspace(fixture.ctx, project.ID, terminal.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRecord.Path, "opencode-result.txt")); !os.IsNotExist(err) {
		t.Fatalf("post-answer result file exists after cancellation: err=%v", err)
	}

	events, err := fixture.database.ListRunEvents(fixture.ctx, project.ID, run.ID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if openCodeEventIndex(events, "run.resumed") >= 0 {
		t.Fatalf("cancelled Question Run unexpectedly resumed: %v", eventTypes(events))
	}
	if openCodeEventIndex(events, "question.cancelled") < 0 {
		t.Fatalf("cancelled Question was not durably cancelled: %v", eventTypes(events))
	}

	betaOption := optionIDByLabel(t, question, "beta")
	if _, err := fixture.services.Questions.Answer(fixture.ctx, project.ID, question.ID, store.QuestionAnswer{
		Kind: "SINGLE_CHOICE", OptionIDs: []string{betaOption},
	}, nil); err == nil {
		t.Fatal("stale answer to cancelled Question unexpectedly succeeded")
	}
	after, err := fixture.database.GetRun(fixture.ctx, project.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "CANCELLED" {
		t.Fatalf("stale Question answer changed cancelled Run to %s", after.Status)
	}
	if _, err := os.Stat(filepath.Join(workspaceRecord.Path, "opencode-result.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale answer produced result file after cancellation: err=%v", err)
	}
	events, err = fixture.database.ListRunEvents(fixture.ctx, project.ID, run.ID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if openCodeEventIndex(events, "run.resumed") >= 0 {
		t.Fatalf("stale answer resumed cancelled Run: %v", eventTypes(events))
	}
}

func newOpenCodeIntegrationFixture(t *testing.T) *openCodeIntegrationFixture {
	t.Helper()
	env := requireOpenCodeIntegrationEnv(t)
	resetRunexecIntegrationDatabase(t, env.databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	database, err := postgres.Open(ctx, env.databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	cipher, err := secrets.NewAESGCM(1, map[int][]byte{1: bytes.Repeat([]byte{0x5a}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	secretService, err := secrets.NewService(database, cipher)
	if err != nil {
		t.Fatal(err)
	}

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
	startOutboundOpenCodeRunner(t, ctx, services, database, env.image)
	config := scheduler.DefaultConfig("opencode-integration")
	config.PollInterval = 20 * time.Millisecond
	config.LeaseDuration = 5 * time.Second
	config.HeartbeatInterval = time.Second
	config.CapacityBackoff = 20 * time.Millisecond
	config.MaxInFlight = 1
	coordinator, err := scheduler.New(services.ExecutionStore, processor, processor, config)
	if err != nil {
		t.Fatal(err)
	}
	services.Scheduler = coordinator

	return &openCodeIntegrationFixture{
		ctx: ctx, database: database, services: services, secretService: secretService,
		repositoryPath: repositoryPath, env: env, coordinator: coordinator,
	}
}

func requireOpenCodeIntegrationEnv(t *testing.T) openCodeIntegrationEnv {
	t.Helper()
	if os.Getenv("AGENT_BOARD_TEST_OPENCODE") != "1" {
		t.Skip("AGENT_BOARD_TEST_OPENCODE=1 is required for the credential-gated OpenCode integration")
	}
	if os.Getenv("AGENT_BOARD_TEST_DOCKER") != "1" {
		t.Skip("AGENT_BOARD_TEST_DOCKER=1 is required for the OpenCode Docker integration")
	}
	env := openCodeIntegrationEnv{
		databaseURL:  strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_DATABASE_URL")),
		apiKey:       strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_API_KEY")),
		providerKind: strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_PROVIDER_KIND")),
		modelName:    strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_MODEL")),
		image:        strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_RUNTIME_IMAGE")),
	}
	if env.databaseURL == "" || env.apiKey == "" || env.providerKind == "" || env.modelName == "" {
		t.Skip("database URL, OpenCode API key, provider kind and model are required")
	}
	if env.image == "" {
		env.image = "agent-board-opencode-runtime:manual"
	}
	return env
}

func startOutboundOpenCodeRunner(t *testing.T, ctx context.Context, services *app.Services, database *postgres.Store, image string) {
	t.Helper()
	created, token, err := createExternalRunnerCredential(ctx, services.ControlPlane)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/runner/ws", services.ControlPlane.Runners.Connections)
	server := httptest.NewUnstartedServer(mux)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	workspaceDir := t.TempDir()
	if err := os.Chmod(workspaceDir, 0o777); err != nil {
		t.Fatal(err)
	}
	dockerHost := os.Getenv("AGENT_BOARD_TEST_DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "host.docker.internal"
	}
	port := listener.Addr().(*net.TCPAddr).Port
	name := "agent-board-opencode-it-" + filepath.Base(t.TempDir())
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--detach",
		"--name", name,
		"--add-host", "host.docker.internal:host-gateway",
		"-e", "AGENT_BOARD_URL=http://"+net.JoinHostPort(dockerHost, strconv.Itoa(port)),
		"-e", "AGENT_RUNNER_ID="+created.ID,
		"-e", "AGENT_RUNNER_TOKEN="+token,
		"-e", "AGENT_RUNNER_WORKSPACE_ROOT=/workspace",
		"-v", workspaceDir+":/workspace",
		image,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker run OpenCode runner: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
	})
	waitForRunnerConnected(t, services.ControlPlane.Runners.Connections, created.ID)
	database.SetRunnerCandidates(services.ControlPlane.Runners.Connections.Candidates)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, id := range services.ControlPlane.Runners.Connections.Candidates(opencode.Name) {
			if id == created.ID {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
	t.Fatalf("live registry did not advertise the OpenCode runner %s: %s", created.ID, logs)
}

func (f *openCodeIntegrationFixture) startScheduler(t *testing.T) {
	t.Helper()
	if f.started {
		t.Fatal("OpenCode integration scheduler already started")
	}
	f.started = true
	schedulerCtx, stopScheduler := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() { done <- f.coordinator.Run(schedulerCtx) }()
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
}

func (f *openCodeIntegrationFixture) createRun(t *testing.T, spec openCodeRunSpec) (store.Project, store.Run) {
	t.Helper()
	apiKey := spec.apiKey
	if apiKey == "" {
		apiKey = f.env.apiKey
	}
	project, err := f.services.ControlPlane.CreateProject(f.ctx, store.Project{
		Name: "OpenCode integration", IssuePrefix: "OC", RepositoryPath: f.repositoryPath,
		DefaultBranch: "main", WorkflowSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	credentialRef := "opencode-integration-provider-key"
	if _, err := f.secretService.Put(f.ctx, secrets.Scope{ProjectID: &project.ID}, credentialRef, []byte(apiKey)); err != nil {
		t.Fatalf("store provider credential: %v", err)
	}
	var baseURL *string
	if value := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_BASE_URL")); value != "" {
		baseURL = &value
	}
	provider, err := f.services.ControlPlane.CreateProvider(f.ctx, store.Provider{
		Name: "OpenCode integration provider", Kind: f.env.providerKind, BaseURL: baseURL,
		CredentialRef: &credentialRef, Enabled: true, HealthStatus: "HEALTHY", SafeMetadata: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := project.ID
	model, err := f.services.ControlPlane.CreateModelProfile(f.ctx, store.ModelProfile{
		ProjectID: &scope, ProviderID: provider.ID, Name: "OpenCode integration model", Model: f.env.modelName,
		GenerationSettings: store.EmptyObject, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := f.services.ControlPlane.CreateAgent(f.ctx, store.Agent{
		ProjectID: &scope, Name: "OpenCode integration agent", RoleInstructions: spec.roleInstructions,
		Engine: opencode.Name, ModelProfileID: model.ID,
		EngineSettings: store.EmptyObject, ConcurrencyLimit: 1, State: "ENABLED",
	})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := f.services.ControlPlane.CreateIssue(f.ctx, store.Issue{
		ProjectID: project.ID, Title: spec.title, Description: spec.description, Status: "TODO",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := f.services.ControlPlane.AssignIssue(f.ctx, project.ID, issue.ID, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	return project, run
}

func waitForOpenCodeQuestion(t *testing.T, ctx context.Context, database *postgres.Store, projectID, runID string) store.Question {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := database.GetRun(ctx, projectID, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == "FAILED" || run.Status == "CANCELLED" || run.Status == "READY_FOR_REVIEW" {
			t.Fatalf("run terminated before Question: status=%s failure=%q", run.Status, openCodeFailureReason(run.FailureReason))
		}
		filterRunID := runID
		questions, err := database.ListQuestions(ctx, projectID, store.QuestionFilter{RunID: &filterRunID, Statuses: []string{"OPEN"}})
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == "WAITING_FOR_INPUT" && len(questions) == 1 {
			events, err := database.ListRunEvents(ctx, projectID, runID, 0, 500)
			if err != nil {
				t.Fatal(err)
			}
			if openCodeEventIndex(events, "question.created") >= 0 && openCodeEventIndex(events, "run.waiting_for_input") >= 0 {
				return questions[0]
			}
		}
		if len(questions) > 1 {
			t.Fatalf("OpenCode asked more than one open Question: %+v", questions)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for OpenCode Question: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitForOpenCodeSessionStatus(t *testing.T, ctx context.Context, database *postgres.Store, projectID, runID, status string) store.ExecutionSession {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		sessions, err := database.ListExecutionSessionsByRun(ctx, projectID, runID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions) == 1 && sessions[0].Status == status {
			return sessions[0]
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for OpenCode session status %s: sessions=%+v", status, sessions)
		case <-ticker.C:
		}
	}
}

func assertOpenCodeServerSession(t *testing.T, ctx context.Context, database *postgres.Store, projectID, runID string, requireCancelled bool) {
	t.Helper()
	sessions, err := database.ListExecutionSessionsByRun(ctx, projectID, runID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || !strings.Contains(string(sessions[0].CommandArgv), "opencode") || !strings.Contains(string(sessions[0].CommandArgv), "serve") {
		t.Fatalf("execution sessions=%+v", sessions)
	}
	if requireCancelled && sessions[0].Status != "CANCELLED" {
		t.Fatalf("execution session status=%s want CANCELLED", sessions[0].Status)
	}
}

func assertOpenCodeWorkspaceFile(t *testing.T, ctx context.Context, database *postgres.Store, projectID, workspaceID, name, want string) {
	t.Helper()
	assertOpenCodeWorkspaceFileEither(t, ctx, database, projectID, workspaceID, name, want)
}

func assertOpenCodeWorkspaceFileEither(t *testing.T, ctx context.Context, database *postgres.Store, projectID, workspaceID, name string, want ...string) {
	t.Helper()
	workspaceRecord, err := database.GetWorkspace(ctx, projectID, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(workspaceRecord.Path, name))
	if err != nil {
		t.Fatalf("read OpenCode result: %v", err)
	}
	for _, candidate := range want {
		if string(content) == candidate {
			return
		}
	}
	t.Fatalf("OpenCode result=%q want one of %q", content, want)
}

func assertOpenCodeSecretAbsent(t *testing.T, fixture *openCodeIntegrationFixture, projectID, runID, secret string) {
	t.Helper()
	snapshot, err := fixture.services.RunEvidence.Inspect(fixture.ctx, projectID, runID)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, value []byte) {
		t.Helper()
		if bytes.Contains(value, []byte(secret)) {
			t.Fatalf("invalid OpenRouter credential leaked through %s", name)
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	check("Run evidence metadata", encoded)
	for _, chunk := range snapshot.RawOutput {
		_, reader, err := fixture.services.RunEvidence.OpenRawOutput(fixture.ctx, projectID, runID, chunk.ID)
		if err != nil {
			t.Fatalf("open raw output %s: %v", chunk.ID, err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read raw output %s: read=%v close=%v", chunk.ID, readErr, closeErr)
		}
		check("raw output blob "+chunk.ID, data)
	}
	for _, artifact := range snapshot.Artifacts {
		_, reader, err := fixture.services.RunEvidence.OpenArtifact(fixture.ctx, projectID, runID, artifact.ID)
		if err != nil {
			t.Fatalf("open artifact %s: %v", artifact.ID, err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read artifact %s: read=%v close=%v", artifact.ID, readErr, closeErr)
		}
		check("artifact blob "+artifact.ID, data)
	}
}

func optionIDByLabel(t *testing.T, question store.Question, label string) string {
	t.Helper()
	var options []store.QuestionOption
	if err := json.Unmarshal(question.Options, &options); err != nil {
		t.Fatalf("decode Question options: %v", err)
	}
	for _, option := range options {
		if strings.EqualFold(strings.TrimSpace(option.Label), label) {
			return option.ID
		}
	}
	t.Fatalf("Question does not contain option %q: %+v", label, options)
	return ""
}

func assertOpenCodeQuestionEventOrder(t *testing.T, events []store.Event) {
	t.Helper()
	types := []string{"question.created", "run.waiting_for_input", "question.answered", "run.resumed"}
	last := -1
	for _, eventType := range types {
		index := openCodeEventIndex(events, eventType)
		if index < 0 || index <= last {
			t.Fatalf("OpenCode Question event order=%v", eventTypes(events))
		}
		last = index
	}
}

func assertOpenCodeEventCount(t *testing.T, events []store.Event, eventType string, want int) {
	t.Helper()
	got := 0
	for _, event := range events {
		if event.Type == eventType {
			got++
		}
	}
	if got != want {
		t.Fatalf("OpenCode %s event count=%d want %d; events=%v", eventType, got, want, eventTypes(events))
	}
}

func openCodeEventIndex(events []store.Event, eventType string) int {
	for index, event := range events {
		if event.Type == eventType {
			return index
		}
	}
	return -1
}

func openCodeFailureReason(reason *string) string {
	if reason == nil {
		return ""
	}
	return *reason
}
