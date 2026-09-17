package runexec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/httpapi"
	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
	"github.com/jackc/pgx/v5"
)

const delegationExecutionEngineName = "delegation-e2e"

type delegationExecutionEngine struct {
	targetAgentID string

	mu                  sync.Mutex
	created             engine.Delegation
	replayed            engine.Delegation
	enabledExecutions   int
	disabledExecutions  int
	delegatedExecutions int
}

func (e *delegationExecutionEngine) Name() string { return delegationExecutionEngineName }

func (e *delegationExecutionEngine) Execute(ctx context.Context, request engine.Request) (engine.Result, error) {
	if request.Context.Delegation != nil {
		if request.Delegation != nil || request.IssueStatus != nil {
			return engine.Result{}, fmt.Errorf("delegated execution received authoritative parent capabilities")
		}
		e.mu.Lock()
		e.delegatedExecutions++
		e.mu.Unlock()
		return engine.Result{Summary: "delegated execution completed"}, nil
	}

	if !request.Context.Agent.AllowDelegation {
		if request.Delegation != nil {
			return engine.Result{}, fmt.Errorf("disabled parent received delegation capability")
		}
		e.mu.Lock()
		e.disabledExecutions++
		e.mu.Unlock()
		return engine.Result{Summary: "delegation capability withheld"}, nil
	}
	if request.Delegation == nil {
		return engine.Result{}, fmt.Errorf("enabled parent did not receive delegation capability")
	}

	delegationRequest := engine.DelegationRequest{
		TargetAgentID: e.targetAgentID,
		Task:          "inspect scheduler ownership",
		RequestKey:    "execution-e2e-request-1",
	}
	created, err := request.Delegation.Delegate(ctx, delegationRequest)
	if err != nil {
		return engine.Result{}, err
	}
	replayed, err := request.Delegation.Delegate(ctx, delegationRequest)
	if err != nil {
		return engine.Result{}, err
	}
	if replayed.ID != created.ID || replayed.RunID != created.RunID {
		return engine.Result{}, fmt.Errorf("idempotent replay created a different delegation: first=%+v replay=%+v", created, replayed)
	}

	e.mu.Lock()
	e.created = created
	e.replayed = replayed
	e.enabledExecutions++
	e.mu.Unlock()
	return engine.Result{Summary: "delegation requested"}, nil
}

func (e *delegationExecutionEngine) snapshot() (engine.Delegation, engine.Delegation, int, int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.created, e.replayed, e.enabledExecutions, e.disabledExecutions, e.delegatedExecutions
}

type delegationExecutionSessions struct{}

func (delegationExecutionSessions) Start(context.Context, string, string, string, app.AuthorizedExecutionRequest) (*app.AuthorizedExecutionProcess, error) {
	return nil, fmt.Errorf("delegation E2E engine must not launch a subprocess")
}

func (delegationExecutionSessions) Attach(context.Context, string, string) (*app.AuthorizedExecutionProcess, error) {
	return nil, fmt.Errorf("delegation E2E engine must not attach a subprocess")
}

func (delegationExecutionSessions) ReconcileAll(context.Context) error { return nil }

func (delegationExecutionSessions) CreateRunnerSession(_ context.Context, projectID, runID, runnerID string) (store.ExecutionSession, error) {
	return store.ExecutionSession{
		ID:        "delegation-e2e-" + runID,
		ProjectID: projectID,
		RunID:     runID,
		RunnerID:  runnerID,
		Status:    "PENDING",
	}, nil
}

type delegationExecutionRunnerClient struct {
	mu       sync.Mutex
	payloads map[string][]byte
}

func newDelegationExecutionRunnerClient() *delegationExecutionRunnerClient {
	return &delegationExecutionRunnerClient{payloads: make(map[string][]byte)}
}

func (c *delegationExecutionRunnerClient) SendTransfer(_ context.Context, sessionID, _, direction string, payload []byte, progress runner.TransferProgressFunc) error {
	if direction == "to_runner" {
		c.mu.Lock()
		c.payloads[sessionID] = append([]byte(nil), payload...)
		c.mu.Unlock()
	}
	if progress != nil && len(payload) > 0 {
		progress(runner.TransferProgress{BytesTransferred: int64(len(payload)), TotalBytes: int64(len(payload))})
	}
	return nil
}

func (c *delegationExecutionRunnerClient) ReceiveTransfer(_ context.Context, sessionID string, progress runner.TransferProgressFunc) (string, []byte, error) {
	c.mu.Lock()
	payload := append([]byte(nil), c.payloads[sessionID]...)
	c.mu.Unlock()
	if len(payload) == 0 {
		return "", nil, fmt.Errorf("delegation E2E runner has no workspace payload for session %s", sessionID)
	}
	if progress != nil {
		progress(runner.TransferProgress{BytesTransferred: int64(len(payload)), TotalBytes: int64(len(payload))})
	}
	return "delegation-e2e-return-" + sessionID, payload, nil
}

func (*delegationExecutionRunnerClient) ConfirmTransferApplied(context.Context, string, string) error {
	return nil
}

type delegationExecutionRunnerConnector struct {
	client runnerClient
}

func (c delegationExecutionRunnerConnector) Connect(context.Context, string, string) (runnerClient, error) {
	return c.client, nil
}

func TestDelegationTrustedExecutionEndToEnd(t *testing.T) {
	baseDatabaseURL := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_DATABASE_URL"))
	if baseDatabaseURL == "" {
		t.Skip("AGENT_BOARD_TEST_DATABASE_URL is required for delegation E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	databaseURL := isolatedDelegationExecutionDatabase(t, ctx, baseDatabaseURL)
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetEngineRegistered(func(name string) bool { return name == delegationExecutionEngineName })

	repositoryPath := createScriptedFixtureRepository(t, ctx)
	repositoryPolicy, err := repository.NewPolicy([]string{filepath.Dir(repositoryPath)})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	materializer, err := workspace.NewMaterializer(database, repositoryPolicy, git, filepath.Join(t.TempDir(), "workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	services, err := app.NewServicesWithRuntimes(database, materializer, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := services.Close(); err != nil {
			t.Errorf("close delegation E2E services: %v", err)
		}
	}()

	router := httpapi.NewRouterWithApplication(services)
	prefix := fmt.Sprintf("D%08X", uint32(time.Now().UnixNano()))
	var project httpapi.ProjectDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects", fmt.Sprintf(`{"name":"Delegation execution E2E","issuePrefix":"%s","repositoryPath":%q,"defaultBranch":"main","workflowSettings":{}}`, prefix, repositoryPath), http.StatusCreated, &project)

	var provider httpapi.ProviderDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/providers", `{"name":"Provider","kind":"test","enabled":true,"safeMetadata":{}}`, http.StatusCreated, &provider)
	var model httpapi.ModelProfileDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/model-profiles", fmt.Sprintf(`{"providerId":"%s","name":"Model","model":"test","generationSettings":{},"enabled":true}`, provider.ID), http.StatusCreated, &model)

	var parent httpapi.AgentDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/agents", fmt.Sprintf(`{"name":"Parent","roleInstructions":"delegate bounded work","engine":%q,"modelProfileId":"%s","engineSettings":{},"concurrencyLimit":1,"allowDelegation":true,"state":"ENABLED"}`, delegationExecutionEngineName, model.ID), http.StatusCreated, &parent)
	if !parent.AllowDelegation {
		t.Fatal("public Agent create did not persist allowDelegation")
	}
	var target httpapi.AgentDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/agents", fmt.Sprintf(`{"name":"Target","roleInstructions":"perform bounded work","engine":%q,"modelProfileId":"%s","engineSettings":{},"concurrencyLimit":1,"allowDelegation":false,"state":"ENABLED"}`, delegationExecutionEngineName, model.ID), http.StatusCreated, &target)

	runnerRecord, err := database.CreateRunner(ctx, store.Runner{Name: "Delegation E2E Runner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ObserveRunner(ctx, runnerRecord.ID, json.RawMessage(`{"max_active_sessions":4}`)); err != nil {
		t.Fatal(err)
	}
	database.SetRunnerCandidates(func(name string) []string {
		if name != delegationExecutionEngineName {
			return nil
		}
		return []string{runnerRecord.ID}
	})

	var issue httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues", `{"title":"Delegate through trusted execution","description":"bounded delegation","status":"TODO"}`, http.StatusCreated, &issue)
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues/"+issue.ID+"/assignment", fmt.Sprintf(`{"assignedTo":{"type":"AGENT","id":"%s"}}`, parent.ID), http.StatusOK, nil)
	var execution httpapi.IssueExecutionStateDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID+"/execution", "", http.StatusOK, &execution)
	if execution.ActiveRun == nil || execution.ActiveRun.AgentID == nil || *execution.ActiveRun.AgentID != parent.ID {
		t.Fatalf("API assignment did not create authoritative parent Run: %+v", execution)
	}
	parentRun := *execution.ActiveRun
	var before httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID, "", http.StatusOK, &before)

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
	testEngine := &delegationExecutionEngine{targetAgentID: target.ID}
	engines, err := engine.NewRegistry(testEngine)
	if err != nil {
		t.Fatal(err)
	}
	runnerClient := newDelegationExecutionRunnerClient()
	processor, err := NewProcessor(
		services.ExecutionStore,
		services.ExecutionContext,
		services.RuntimeInstances,
		delegationExecutionSessions{},
		engines,
		recorder,
		output,
		git,
		delegationExecutionRunnerConnector{client: runnerClient},
	)
	if err != nil {
		t.Fatal(err)
	}
	processor.SetWorkspaceEnsurer(services.Workspaces)
	config := scheduler.DefaultConfig("delegation-api-e2e")
	config.PollInterval = 10 * time.Millisecond
	config.LeaseDuration = 3 * time.Second
	config.HeartbeatInterval = 500 * time.Millisecond
	config.CapacityBackoff = 10 * time.Millisecond
	config.MaxInFlight = 1
	config.PersistedEvents = processor
	coordinator, err := scheduler.New(services.ExecutionStore, processor, processor, config)
	if err != nil {
		t.Fatal(err)
	}
	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	schedulerDone := make(chan error, 1)
	go func() {
		schedulerDone <- coordinator.Run(schedulerCtx)
		close(schedulerDone)
	}()
	defer func() {
		stopScheduler()
		select {
		case err := <-schedulerDone:
			if err != nil {
				t.Errorf("delegation E2E scheduler stopped with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("delegation E2E scheduler did not stop")
		}
	}()

	parentTerminal := waitForDelegationExecutionRun(t, ctx, router, project.ID, parentRun.ID)
	if parentTerminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("parent Run status=%s want READY_FOR_REVIEW", parentTerminal.Status)
	}
	created, replayed, enabledExecutions, disabledExecutions, delegatedExecutions := testEngine.snapshot()
	if enabledExecutions != 1 || disabledExecutions != 0 || delegatedExecutions != 0 {
		t.Fatalf("unexpected Engine executions enabled=%d disabled=%d delegated=%d", enabledExecutions, disabledExecutions, delegatedExecutions)
	}
	if created.ID == "" || created.RunID == "" || replayed.ID != created.ID || replayed.RunID != created.RunID {
		t.Fatalf("Engine delegation replay was not stable: first=%+v replay=%+v", created, replayed)
	}

	var listed []httpapi.DelegationDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs/"+parentRun.ID+"/delegations", "", http.StatusOK, &listed)
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].DelegatedRunID != created.RunID {
		t.Fatalf("parent delegation list=%+v", listed)
	}
	var lineage httpapi.DelegationDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs/"+created.RunID+"/delegation", "", http.StatusOK, &lineage)
	if lineage.ParentRunID != parentRun.ID || lineage.ParentAgentID != parent.ID || lineage.TargetAgentID != target.ID || lineage.IssueID != issue.ID {
		t.Fatalf("child lineage=%+v", lineage)
	}

	childTerminal := waitForDelegationExecutionRun(t, ctx, router, project.ID, created.RunID)
	if childTerminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("delegated Run status=%s want READY_FOR_REVIEW", childTerminal.Status)
	}
	_, _, enabledExecutions, disabledExecutions, delegatedExecutions = testEngine.snapshot()
	if enabledExecutions != 1 || disabledExecutions != 0 || delegatedExecutions != 1 {
		t.Fatalf("unexpected Engine executions after child enabled=%d disabled=%d delegated=%d", enabledExecutions, disabledExecutions, delegatedExecutions)
	}

	var runs []httpapi.RunDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs", "", http.StatusOK, &runs)
	children := 0
	for _, run := range runs {
		if run.IssueID != issue.ID || run.AgentID == nil || *run.AgentID != target.ID {
			continue
		}
		children++
		if run.ID != created.RunID || run.WorkspaceID != parentRun.WorkspaceID || run.Status != "READY_FOR_REVIEW" {
			t.Fatalf("ordinary delegated Run=%+v", run)
		}
	}
	if children != 1 {
		t.Fatalf("delegated Run count=%d want 1", children)
	}

	jobCount, jobState := delegationExecutionStartJob(t, ctx, databaseURL, project.ID, created.RunID)
	if jobCount != 1 || jobState != "DONE" {
		t.Fatalf("delegated START scheduler jobs count=%d state=%q want one DONE job", jobCount, jobState)
	}

	var afterSchedule httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID, "", http.StatusOK, &afterSchedule)
	if afterSchedule.Status != before.Status || afterSchedule.AssignedTo == nil || before.AssignedTo == nil || *afterSchedule.AssignedTo != *before.AssignedTo {
		t.Fatalf("delegation execution changed Issue authority: before=%+v after=%+v", before, afterSchedule)
	}

	parent.AllowDelegation = false
	delegationExecutionJSON(t, router, http.MethodPut, "/api/projects/"+project.ID+"/agents/"+parent.ID, fmt.Sprintf(`{"name":%q,"roleInstructions":%q,"engine":%q,"modelProfileId":"%s","engineSettings":{},"concurrencyLimit":%d,"allowDelegation":false,"state":%q}`, parent.Name, parent.RoleInstructions, parent.Engine, parent.ModelProfileID, parent.ConcurrencyLimit, parent.State), http.StatusOK, &parent)
	if parent.AllowDelegation {
		t.Fatal("public Agent update did not disable delegation")
	}
	var deniedIssue httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues", `{"title":"Disabled delegation","description":"must reject","status":"TODO"}`, http.StatusCreated, &deniedIssue)
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID+"/assignment", fmt.Sprintf(`{"assignedTo":{"type":"AGENT","id":"%s"}}`, parent.ID), http.StatusOK, nil)
	var deniedExecution httpapi.IssueExecutionStateDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID+"/execution", "", http.StatusOK, &deniedExecution)
	if deniedExecution.ActiveRun == nil || deniedExecution.ActiveRun.AgentID == nil || *deniedExecution.ActiveRun.AgentID != parent.ID {
		t.Fatalf("disabled-policy fixture has no authoritative parent Run: %+v", deniedExecution)
	}
	deniedTerminal := waitForDelegationExecutionRun(t, ctx, router, project.ID, deniedExecution.ActiveRun.ID)
	if deniedTerminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("disabled-policy parent status=%s want READY_FOR_REVIEW", deniedTerminal.Status)
	}
	_, _, enabledExecutions, disabledExecutions, delegatedExecutions = testEngine.snapshot()
	if enabledExecutions != 1 || disabledExecutions != 1 || delegatedExecutions != 1 {
		t.Fatalf("disabled execution did not observe withheld capability: enabled=%d disabled=%d delegated=%d", enabledExecutions, disabledExecutions, delegatedExecutions)
	}
	var denied []httpapi.DelegationDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs/"+deniedExecution.ActiveRun.ID+"/delegations", "", http.StatusOK, &denied)
	if len(denied) != 0 {
		t.Fatalf("disabled parent created lineage: %+v", denied)
	}
	var afterDisabledRuns []httpapi.RunDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs", "", http.StatusOK, &afterDisabledRuns)
	for _, run := range afterDisabledRuns {
		if run.IssueID == deniedIssue.ID && run.AgentID != nil && *run.AgentID == target.ID {
			t.Fatalf("disabled delegation created target Run: %+v", run)
		}
	}
	var afterDisabledIssue httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID, "", http.StatusOK, &afterDisabledIssue)
	if afterDisabledIssue.Status != deniedIssue.Status || afterDisabledIssue.AssignedTo == nil || afterDisabledIssue.AssignedTo.Type != "AGENT" || afterDisabledIssue.AssignedTo.ID != parent.ID {
		t.Fatalf("disabled delegation changed Issue authority: created=%+v after=%+v", deniedIssue, afterDisabledIssue)
	}
}

func waitForDelegationExecutionRun(t *testing.T, ctx context.Context, router http.Handler, projectID, runID string) httpapi.RunDTO {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var last httpapi.RunDTO
	for {
		var runs []httpapi.RunDTO
		delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+projectID+"/runs", "", http.StatusOK, &runs)
		for _, run := range runs {
			if run.ID != runID {
				continue
			}
			last = run
			switch run.Status {
			case "READY_FOR_REVIEW", "FAILED", "CANCELLED":
				return run
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for Run %s: %v last=%+v", runID, ctx.Err(), last)
		case <-ticker.C:
		}
	}
}

func delegationExecutionStartJob(t *testing.T, ctx context.Context, databaseURL, projectID, runID string) (int, string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var count int
	var state string
	if err := conn.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(state), '')
		FROM scheduler_jobs
		WHERE project_id=$1 AND run_id=$2 AND kind='START'
	`, projectID, runID).Scan(&count, &state); err != nil {
		t.Fatal(err)
	}
	return count, state
}

func isolatedDelegationExecutionDatabase(t *testing.T, ctx context.Context, baseDatabaseURL string) string {
	t.Helper()
	parsed, err := url.Parse(baseDatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("agent_board_delegation_execution_%x", uint64(time.Now().UnixNano()))
	admin, err := pgx.Connect(ctx, baseDatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+databaseName); err != nil {
		_ = admin.Close(ctx)
		t.Fatal(err)
	}
	if err := admin.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanup, connectErr := pgx.Connect(cleanupCtx, baseDatabaseURL)
		if connectErr != nil {
			t.Errorf("connect to drop delegation execution database: %v", connectErr)
			return
		}
		defer cleanup.Close(cleanupCtx)
		if _, dropErr := cleanup.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+databaseName+" WITH (FORCE)"); dropErr != nil {
			t.Errorf("drop delegation execution database: %v", dropErr)
		}
	})

	parsed.Path = "/" + databaseName
	isolatedURL := parsed.String()
	schemaConfig, err := pgx.ParseConfig(isolatedURL)
	if err != nil {
		t.Fatal(err)
	}
	schemaConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	schemaConn, err := pgx.ConnectConfig(ctx, schemaConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer schemaConn.Close(ctx)
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "database", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schemaConn.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply isolated delegation execution schema: %v", err)
	}
	return isolatedURL
}

func delegationExecutionJSON(t *testing.T, router http.Handler, method, path, body string, wantStatus int, target any) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.Code, wantStatus, response.Body.String())
	}
	if target != nil && response.Body.Len() != 0 {
		if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
			t.Fatalf("decode %s %s response: %v body=%s", method, path, err, response.Body.String())
		}
	}
}
