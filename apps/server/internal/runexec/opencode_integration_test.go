package runexec

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/repository"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	dockerruntime "github.com/brantje/agent-board/apps/server/internal/runtime/docker"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func TestOpenCodeDockerInteractiveQuestionRoundTrip(t *testing.T) {
	if os.Getenv("AGENT_BOARD_TEST_OPENCODE") != "1" {
		t.Skip("AGENT_BOARD_TEST_OPENCODE=1 is required for the credential-gated OpenCode integration")
	}
	if os.Getenv("AGENT_BOARD_TEST_DOCKER") != "1" {
		t.Skip("AGENT_BOARD_TEST_DOCKER=1 is required for the OpenCode Docker integration")
	}
	databaseURL := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_DATABASE_URL"))
	apiKey := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_API_KEY"))
	providerKind := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_PROVIDER_KIND"))
	modelName := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_MODEL"))
	if databaseURL == "" || apiKey == "" || providerKind == "" || modelName == "" {
		t.Skip("database URL, OpenCode API key, provider kind and model are required")
	}
	image := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_RUNTIME_IMAGE"))
	if image == "" {
		image = "agent-board-opencode-runtime:manual"
	}

	resetRunexecIntegrationDatabase(t, databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
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
	dockerRuntime, err := dockerruntime.New()
	if err != nil {
		t.Fatal(err)
	}
	services, err := app.NewServicesWithRuntimes(
		database,
		materializer,
		map[string]runtimepkg.Implementation{"docker": dockerRuntime},
		secretService,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := services.Close(); err != nil {
			t.Errorf("close services: %v", err)
		}
	})

	project, run := createOpenCodeIntegrationRun(t, ctx, services.ControlPlane, secretService, repositoryPath, image, providerKind, modelName, apiKey)
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
	engines, err := engine.NewRegistry(opencode.New())
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewProcessor(
		services.ExecutionStore,
		services.ExecutionContext,
		services.RuntimeInstances,
		services.ExecutionSessions,
		engines,
		recorder,
		output,
		candidate,
	)
	if err != nil {
		t.Fatal(err)
	}

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
	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- coordinator.Run(schedulerCtx) }()
	defer func() {
		stopScheduler()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("scheduler did not stop")
		}
	}()

	question := waitForOpenCodeQuestion(t, ctx, database, project.ID, run.ID)
	if question.Kind != "SINGLE_CHOICE" {
		t.Fatalf("native OpenCode Question kind=%q want SINGLE_CHOICE", question.Kind)
	}
	betaOption := optionIDByLabel(t, question, "beta")
	if _, err := services.Questions.Answer(ctx, project.ID, question.ID, store.QuestionAnswer{
		Kind:      "SINGLE_CHOICE",
		OptionIDs: []string{betaOption},
	}, nil); err != nil {
		t.Fatalf("answer OpenCode Question through Agent Board: %v", err)
	}

	terminal := waitForScriptedRun(t, ctx, database, project.ID, run.ID)
	if terminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("run status=%s failure=%v", terminal.Status, terminal.FailureReason)
	}
	workspaceRecord, err := database.GetWorkspace(ctx, project.ID, terminal.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := os.ReadFile(filepath.Join(workspaceRecord.Path, "opencode-result.txt"))
	if err != nil {
		t.Fatalf("read OpenCode result: %v", err)
	}
	if string(result) != "beta\n" {
		t.Fatalf("OpenCode result=%q want %q", result, "beta\\n")
	}

	events, err := database.ListRunEvents(ctx, project.ID, run.ID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	assertOpenCodeQuestionEventOrder(t, events)
	sessions, err := database.ListExecutionSessions(ctx, run.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || !strings.Contains(string(sessions[0].CommandArgv), "opencode") || !strings.Contains(string(sessions[0].CommandArgv), "serve") {
		t.Fatalf("execution sessions=%+v", sessions)
	}
}

func createOpenCodeIntegrationRun(
	t *testing.T,
	ctx context.Context,
	control *app.Service,
	secretService *secrets.Service,
	repositoryPath, image, providerKind, modelName, apiKey string,
) (store.Project, store.Run) {
	t.Helper()
	project, err := control.CreateProject(ctx, store.Project{
		Name:             "OpenCode integration",
		RepositoryPath:   repositoryPath,
		DefaultBranch:    "main",
		WorkflowSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	credentialRef := "opencode-integration-provider-key"
	if _, err := secretService.Put(ctx, secrets.Scope{ProjectID: &project.ID}, credentialRef, []byte(apiKey)); err != nil {
		t.Fatalf("store provider credential: %v", err)
	}
	var baseURL *string
	if value := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_BASE_URL")); value != "" {
		baseURL = &value
	}
	provider, err := control.CreateProvider(ctx, store.Provider{
		Name:          "OpenCode integration provider",
		Kind:          providerKind,
		BaseURL:       baseURL,
		CredentialRef: &credentialRef,
		Enabled:       true,
		HealthStatus:  "HEALTHY",
		SafeMetadata:  store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := project.ID
	model, err := control.CreateModelProfile(ctx, store.ModelProfile{
		ProjectID:          &scope,
		ProviderID:         provider.ID,
		Name:               "OpenCode integration model",
		Model:              modelName,
		GenerationSettings: store.EmptyObject,
		Enabled:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeConfig, err := control.CreateRuntime(ctx, store.Runtime{
		ProjectID:       &scope,
		Name:            "OpenCode integration runtime",
		Kind:            "docker",
		Image:           image,
		NetworkPolicy:   "outbound",
		WorkspacePolicy: "issue",
		Capabilities:    store.EmptyObject,
		Enabled:         true,
		HealthStatus:    "HEALTHY",
	})
	if err != nil {
		t.Fatal(err)
	}
	executorProfile, err := control.CreateExecutorProfile(ctx, store.ExecutorProfile{
		ProjectID:      &scope,
		Name:           "OpenCode integration executor",
		Engine:         opencode.Name,
		ModelProfileID: model.ID,
		RuntimeID:      runtimeConfig.ID,
		EngineSettings: store.EmptyObject,
		Enabled:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := control.CreateAgent(ctx, store.Agent{
		ProjectID:         &scope,
		Name:              "OpenCode integration agent",
		RoleInstructions:  "Follow the issue instructions exactly. Use OpenCode's native Question tool for the requested human choice and wait for the answer before editing.",
		ExecutorProfileID: executorProfile.ID,
		ConcurrencyLimit:  1,
		State:             "ENABLED",
	})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := control.CreateIssue(ctx, store.Issue{
		ProjectID: project.ID,
		Title:     "Prove the native OpenCode Question round trip",
		Description: "Before changing any files, use OpenCode's native Question tool to ask exactly one blocking single-choice Question: 'Which marker should I write?' with options 'alpha' and 'beta'. Do not use a free-form answer. After the human answer, create opencode-result.txt containing exactly the selected marker followed by a newline. Do not ask any other Question and do not modify other files.",
		Status:    "TODO",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := control.AssignIssue(ctx, project.ID, issue.ID, agent.ID)
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
		if run.Status == "FAILED" || run.Status == "CANCELLED" {
			t.Fatalf("run terminated before Question: status=%s failure=%v", run.Status, run.FailureReason)
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

func openCodeEventIndex(events []store.Event, eventType string) int {
	for index, event := range events {
		if event.Type == eventType {
			return index
		}
	}
	return -1
}
