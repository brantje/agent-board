package runexec

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
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
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func TestOpenCodeConcurrentRunnerSessionsNormalAndQuestions(t *testing.T) {
	fixture := newConcurrentOpenCodeIntegrationFixture(t, 3)
	project, agent := createConcurrentOpenCodeProject(t, fixture)

	questionARun := createConcurrentOpenCodeRun(t, fixture, project, agent,
		"Concurrent native Question A",
		"Before changing any files, use OpenCode's native Question tool to ask exactly one blocking single-choice Question: 'Which marker should question A write?' with options 'amber' and 'violet'. Do not use a free-form answer. After the human answer, create question-a.txt containing only the selected marker; a single trailing newline is allowed. Do not ask any other Question and do not modify other files.",
	)
	questionBRun := createConcurrentOpenCodeRun(t, fixture, project, agent,
		"Concurrent native Question B",
		"Before changing any files, use OpenCode's native Question tool to ask exactly one blocking single-choice Question: 'Which marker should question B write?' with options 'circle' and 'square'. Do not use a free-form answer. After the human answer, create question-b.txt containing only the selected marker; a single trailing newline is allowed. Do not ask any other Question and do not modify other files.",
	)
	fixture.startScheduler(t)

	questionA := waitForOpenCodeQuestion(t, fixture.ctx, fixture.database, project.ID, questionARun.ID)
	questionB := waitForOpenCodeQuestion(t, fixture.ctx, fixture.database, project.ID, questionBRun.ID)
	if questionA.ID == questionB.ID {
		t.Fatalf("concurrent Runs unexpectedly share Question %s", questionA.ID)
	}

	normalRun := createConcurrentOpenCodeRun(t, fixture, project, agent,
		"Concurrent normal coding Run",
		"Do not ask any Question. Create concurrent-normal.txt containing exactly concurrent-normal-ok with no trailing newline. Do not modify any other file. After writing that file, stop.",
	)

	// The two Question Runs must retain their live native OpenCode processes
	// while the normal coding Run starts on a third capacity-1 Runner. Check the
	// Execution Sessions before waiting for the normal Run to finish so this is
	// a direct concurrency assertion rather than an inference from final state.
	questionASession := waitForOpenCodeSessionStatus(t, fixture.ctx, fixture.database, project.ID, questionARun.ID, "RUNNING")
	questionBSession := waitForOpenCodeSessionStatus(t, fixture.ctx, fixture.database, project.ID, questionBRun.ID, "RUNNING")
	normalSession := waitForOpenCodeSessionStatus(t, fixture.ctx, fixture.database, project.ID, normalRun.ID, "RUNNING")
	if questionASession.RunnerID == "" || questionBSession.RunnerID == "" || normalSession.RunnerID == "" {
		t.Fatalf("concurrent sessions missing Runner ownership: A=%+v B=%+v normal=%+v", questionASession, questionBSession, normalSession)
	}
	if questionASession.RunnerID == questionBSession.RunnerID || questionASession.RunnerID == normalSession.RunnerID || questionBSession.RunnerID == normalSession.RunnerID {
		t.Fatalf("capacity-1 Runners reused across simultaneously running sessions: A=%s B=%s normal=%s", questionASession.RunnerID, questionBSession.RunnerID, normalSession.RunnerID)
	}

	normalTerminal := waitForScriptedRun(t, fixture.ctx, fixture.database, project.ID, normalRun.ID)
	if normalTerminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("normal concurrent Run status=%s failure=%q", normalTerminal.Status, openCodeFailureReason(normalTerminal.FailureReason))
	}
	assertOpenCodeWorkspaceFile(t, fixture.ctx, fixture.database, project.ID, normalTerminal.WorkspaceID, "concurrent-normal.txt", "concurrent-normal-ok")

	for name, runID := range map[string]string{"question A": questionARun.ID, "question B": questionBRun.ID} {
		run, err := fixture.database.GetRun(fixture.ctx, project.ID, runID)
		if err != nil {
			t.Fatalf("get %s Run: %v", name, err)
		}
		if run.Status != "WAITING_FOR_INPUT" {
			t.Fatalf("%s Run status=%s while normal Run completed; want WAITING_FOR_INPUT", name, run.Status)
		}
	}

	filterNormalRunID := normalRun.ID
	normalQuestions, err := fixture.database.ListQuestions(fixture.ctx, project.ID, store.QuestionFilter{RunID: &filterNormalRunID})
	if err != nil {
		t.Fatal(err)
	}
	if len(normalQuestions) != 0 {
		t.Fatalf("normal concurrent Run unexpectedly created Questions: %+v", normalQuestions)
	}

	type questionAnswer struct {
		question store.Question
		label    string
	}
	answers := []questionAnswer{{question: questionA, label: "violet"}, {question: questionB, label: "square"}}
	errCh := make(chan error, len(answers))
	for _, answer := range answers {
		answer := answer
		optionID := optionIDByLabel(t, answer.question, answer.label)
		go func() {
			_, err := fixture.services.Questions.Answer(fixture.ctx, project.ID, answer.question.ID, store.QuestionAnswer{
				Kind: "SINGLE_CHOICE", OptionIDs: []string{optionID},
			}, nil)
			errCh <- err
		}()
	}
	for range answers {
		if err := <-errCh; err != nil {
			t.Fatalf("answer concurrent OpenCode Question: %v", err)
		}
	}

	questionATerminal := waitForScriptedRun(t, fixture.ctx, fixture.database, project.ID, questionARun.ID)
	questionBTerminal := waitForScriptedRun(t, fixture.ctx, fixture.database, project.ID, questionBRun.ID)
	if questionATerminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("question A Run status=%s failure=%q", questionATerminal.Status, openCodeFailureReason(questionATerminal.FailureReason))
	}
	if questionBTerminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("question B Run status=%s failure=%q", questionBTerminal.Status, openCodeFailureReason(questionBTerminal.FailureReason))
	}
	assertOpenCodeWorkspaceFileEither(t, fixture.ctx, fixture.database, project.ID, questionATerminal.WorkspaceID, "question-a.txt", "violet", "violet\n")
	assertOpenCodeWorkspaceFileEither(t, fixture.ctx, fixture.database, project.ID, questionBTerminal.WorkspaceID, "question-b.txt", "square", "square\n")

	for name, runID := range map[string]string{"question A": questionARun.ID, "question B": questionBRun.ID} {
		events, err := fixture.database.ListRunEvents(fixture.ctx, project.ID, runID, 0, 500)
		if err != nil {
			t.Fatalf("list %s events: %v", name, err)
		}
		assertOpenCodeQuestionEventOrder(t, events)
		for _, eventType := range []string{"question.created", "run.waiting_for_input", "question.answered", "run.resumed"} {
			assertOpenCodeEventCount(t, events, eventType, 1)
		}
	}
}

func newConcurrentOpenCodeIntegrationFixture(t *testing.T, runnerCount int) *openCodeIntegrationFixture {
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

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runner/ws" {
			http.NotFound(w, r)
			return
		}
		services.ControlPlane.Runners.Connections.ServeHTTP(w, r)
	})
	server := startOpenCodeRunnerWSServer(t, handler)
	binary := buildAgentRunnerBinary(t)
	runnerIDs := make([]string, 0, runnerCount)
	for index := range runnerCount {
		created, token, err := services.ControlPlane.Runners.Create(ctx, fmt.Sprintf("OpenCode concurrency runner %d", index+1))
		if err != nil {
			t.Fatal(err)
		}
		runnerWorkspaceRoot := filepath.Join(t.TempDir(), "runner-workspaces")
		if err := os.MkdirAll(runnerWorkspaceRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		proc := startOpenCodeAgentRunner(t, binary, server.URL, created.ID, token, runnerWorkspaceRoot)
		t.Cleanup(func() {
			if proc.Process != nil {
				_ = proc.Process.Kill()
			}
			_ = proc.Wait()
		})
		waitForRunnerConnected(t, services.ControlPlane.Runners.Connections, created.ID)
		runnerIDs = append(runnerIDs, created.ID)
	}
	database.SetRunnerCandidates(services.ControlPlane.Runners.Connections.Candidates)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		candidateSet := make(map[string]struct{})
		for _, id := range services.ControlPlane.Runners.Connections.Candidates(opencode.Name) {
			candidateSet[id] = struct{}{}
		}
		allPresent := true
		for _, id := range runnerIDs {
			if _, ok := candidateSet[id]; !ok {
				allPresent = false
				break
			}
		}
		if allPresent {
			goto runnersReady
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("live registry did not advertise all OpenCode concurrency runners: want=%v got=%v", runnerIDs, services.ControlPlane.Runners.Connections.Candidates(opencode.Name))

runnersReady:
	config := scheduler.DefaultConfig("opencode-concurrency-integration")
	config.PollInterval = 20 * time.Millisecond
	config.LeaseDuration = 5 * time.Second
	config.HeartbeatInterval = time.Second
	config.CapacityBackoff = 20 * time.Millisecond
	config.MaxInFlight = runnerCount
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

func createConcurrentOpenCodeProject(t *testing.T, fixture *openCodeIntegrationFixture) (store.Project, store.Agent) {
	t.Helper()
	project, err := fixture.services.ControlPlane.CreateProject(fixture.ctx, store.Project{
		Name: "OpenCode concurrent integration", IssuePrefix: "OCC", RepositoryPath: fixture.repositoryPath,
		DefaultBranch: "main", WorkflowSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	credentialRef := "opencode-concurrency-provider-key"
	if _, err := fixture.secretService.Put(fixture.ctx, secrets.Scope{ProjectID: &project.ID}, credentialRef, []byte(fixture.env.apiKey)); err != nil {
		t.Fatalf("store provider credential: %v", err)
	}
	var baseURL *string
	if value := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_OPENCODE_BASE_URL")); value != "" {
		baseURL = &value
	}
	provider, err := fixture.services.ControlPlane.CreateProvider(fixture.ctx, store.Provider{
		Name: "OpenCode concurrency provider", Kind: fixture.env.providerKind, BaseURL: baseURL,
		CredentialRef: &credentialRef, Enabled: true, HealthStatus: "HEALTHY", SafeMetadata: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := project.ID
	model, err := fixture.services.ControlPlane.CreateModelProfile(fixture.ctx, store.ModelProfile{
		ProjectID: &scope, ProviderID: provider.ID, Name: "OpenCode concurrency model", Model: fixture.env.modelName,
		GenerationSettings: store.EmptyObject, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := fixture.services.ControlPlane.CreateAgent(fixture.ctx, store.Agent{
		ProjectID: &scope,
		Name:      "OpenCode concurrency agent",
		RoleInstructions: "Follow each issue exactly. Use OpenCode's native Question tool only when the issue explicitly requires human input, and keep every Run isolated from other concurrent Runs.",
		Engine: opencode.Name, ModelProfileID: model.ID,
		EngineSettings: store.EmptyObject, ConcurrencyLimit: 3, State: "ENABLED",
	})
	if err != nil {
		t.Fatal(err)
	}
	return project, agent
}

func createConcurrentOpenCodeRun(t *testing.T, fixture *openCodeIntegrationFixture, project store.Project, agent store.Agent, title, description string) store.Run {
	t.Helper()
	issue, err := fixture.services.ControlPlane.CreateIssue(fixture.ctx, store.Issue{
		ProjectID: project.ID, Title: title, Description: description, Status: "TODO",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := fixture.services.ControlPlane.AssignIssue(fixture.ctx, project.ID, issue.ID, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func singleOpenCodeExecutionSession(t *testing.T, fixture *openCodeIntegrationFixture, projectID, runID string) store.ExecutionSession {
	t.Helper()
	sessions, err := fixture.database.ListExecutionSessionsByRun(fixture.ctx, projectID, runID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("Run %s execution sessions=%+v", runID, sessions)
	}
	return sessions[0]
}
