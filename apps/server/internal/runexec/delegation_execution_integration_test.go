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
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/httpapi"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/jackc/pgx/v5"
)

func TestDelegationTrustedExecutionEndToEnd(t *testing.T) {
	baseDatabaseURL := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_DATABASE_URL"))
	if baseDatabaseURL == "" {
		t.Skip("AGENT_BOARD_TEST_DATABASE_URL is required for delegation E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	databaseURL := isolatedDelegationExecutionDatabase(t, ctx, baseDatabaseURL)
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetEngineRegistered(func(name string) bool { return name == "scripted" })

	router := httpapi.NewRouter(app.New(database))
	prefix := fmt.Sprintf("D%08X", uint32(time.Now().UnixNano()))
	var project httpapi.ProjectDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects", fmt.Sprintf(`{"name":"Delegation execution E2E","issuePrefix":"%s","repositoryPath":%q,"defaultBranch":"main","workflowSettings":{}}`, prefix, t.TempDir()), http.StatusCreated, &project)

	var provider httpapi.ProviderDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/providers", `{"name":"Provider","kind":"test","enabled":true,"safeMetadata":{}}`, http.StatusCreated, &provider)
	var model httpapi.ModelProfileDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/model-profiles", fmt.Sprintf(`{"providerId":"%s","name":"Model","model":"test","generationSettings":{},"enabled":true}`, provider.ID), http.StatusCreated, &model)

	var parent httpapi.AgentDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/agents", fmt.Sprintf(`{"name":"Parent","roleInstructions":"delegate bounded work","engine":"scripted","modelProfileId":"%s","engineSettings":{},"concurrencyLimit":1,"allowDelegation":true,"state":"ENABLED"}`, model.ID), http.StatusCreated, &parent)
	if !parent.AllowDelegation {
		t.Fatal("public Agent create did not persist allowDelegation")
	}
	var target httpapi.AgentDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/agents", fmt.Sprintf(`{"name":"Target","roleInstructions":"perform bounded work","engine":"scripted","modelProfileId":"%s","engineSettings":{},"concurrencyLimit":1,"allowDelegation":false,"state":"ENABLED"}`, model.ID), http.StatusCreated, &target)

	runner, err := database.CreateRunner(ctx, store.Runner{Name: "Delegation E2E Runner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ObserveRunner(ctx, runner.ID, json.RawMessage(`{"max_active_sessions":4}`)); err != nil {
		t.Fatal(err)
	}
	database.SetRunnerCandidates(func(name string) []string {
		if name != "scripted" {
			return nil
		}
		return []string{runner.ID}
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
	parentAdmission := admitDelegationExecutionRun(t, ctx, database, parentRun.ID, "delegation-parent")

	resolver, err := executioncontext.NewResolver(database)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve(ctx, project.ID, parentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Safe.Run.ID != parentRun.ID || resolved.Safe.Agent.ID != parent.ID || !resolved.Safe.Agent.AllowDelegation || resolved.Safe.Delegation != nil {
		t.Fatalf("unexpected trusted parent context: %+v", resolved.Safe)
	}

	var before httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID, "", http.StatusOK, &before)
	requester := newDelegationRequester(database, nil, resolved.Safe)
	if requester == nil {
		t.Fatal("trusted execution did not receive delegation requester")
	}
	request := engine.DelegationRequest{TargetAgentID: target.ID, Task: "inspect scheduler ownership", RequestKey: "execution-e2e-request-1"}
	created, err := requester.Delegate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := requester.Delegate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != created.ID || replayed.RunID != created.RunID {
		t.Fatalf("idempotent replay created a different delegation: first=%+v replay=%+v", created, replayed)
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

	var runs []httpapi.RunDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs", "", http.StatusOK, &runs)
	children := 0
	for _, run := range runs {
		if run.IssueID != issue.ID || run.ID == parentRun.ID {
			continue
		}
		children++
		if run.ID != created.RunID || run.AgentID == nil || *run.AgentID != target.ID || run.WorkspaceID != parentRun.WorkspaceID || run.Status != "QUEUED" {
			t.Fatalf("ordinary delegated Run=%+v", run)
		}
	}
	if children != 1 {
		t.Fatalf("delegated Run count=%d want 1", children)
	}

	var afterRequest httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID, "", http.StatusOK, &afterRequest)
	if afterRequest.Status != before.Status || afterRequest.AssignedTo == nil || before.AssignedTo == nil || *afterRequest.AssignedTo != *before.AssignedTo {
		t.Fatalf("delegation changed Issue authority: before=%+v after=%+v", before, afterRequest)
	}

	completeDelegationExecutionRun(t, ctx, database, parentAdmission, "COMPLETED")
	childAdmission := admitDelegationExecutionRun(t, ctx, database, created.RunID, "delegation-child")
	if childAdmission.Run.ID != created.RunID || childAdmission.AgentID != target.ID {
		t.Fatalf("scheduler admitted wrong delegated execution: %+v", childAdmission)
	}
	var runningChild store.Run
	runningChild, err = database.GetRun(ctx, project.ID, created.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if runningChild.Status != "RUNNING" {
		t.Fatalf("delegated Run status=%s want RUNNING", runningChild.Status)
	}
	var afterSchedule httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID, "", http.StatusOK, &afterSchedule)
	if afterSchedule.Status != before.Status || afterSchedule.AssignedTo == nil || *afterSchedule.AssignedTo != *before.AssignedTo {
		t.Fatalf("normal child scheduling changed Issue authority: before=%+v after=%+v", before, afterSchedule)
	}

	parent.AllowDelegation = false
	delegationExecutionJSON(t, router, http.MethodPut, "/api/projects/"+project.ID+"/agents/"+parent.ID, fmt.Sprintf(`{"name":%q,"roleInstructions":%q,"engine":%q,"modelProfileId":"%s","engineSettings":{},"concurrencyLimit":%d,"allowDelegation":false,"state":%q}`, parent.Name, parent.RoleInstructions, parent.Engine, parent.ModelProfileID, parent.ConcurrencyLimit, parent.State), http.StatusOK, &parent)
	var deniedIssue httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues", `{"title":"Disabled delegation","description":"must reject","status":"TODO"}`, http.StatusCreated, &deniedIssue)
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID+"/assignment", fmt.Sprintf(`{"assignedTo":{"type":"AGENT","id":"%s"}}`, parent.ID), http.StatusOK, nil)
	var deniedExecution httpapi.IssueExecutionStateDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID+"/execution", "", http.StatusOK, &deniedExecution)
	if deniedExecution.ActiveRun == nil {
		t.Fatal("disabled-policy fixture has no parent Run")
	}
	admitDelegationExecutionRun(t, ctx, database, deniedExecution.ActiveRun.ID, "delegation-disabled")
	deniedResolved, err := resolver.Resolve(ctx, project.ID, deniedExecution.ActiveRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deniedResolved.Safe.Agent.AllowDelegation {
		t.Fatal("disabled parent resolved with delegation policy enabled")
	}
	deniedRequester := newDelegationRequester(database, nil, deniedResolved.Safe)
	if deniedRequester == nil {
		t.Fatal("trusted execution requester is unavailable")
	}
	if _, err := deniedRequester.Delegate(ctx, engine.DelegationRequest{TargetAgentID: target.ID, Task: "must reject", RequestKey: "disabled"}); err == nil {
		t.Fatal("disabled delegation policy accepted request")
	}
	var denied []httpapi.DelegationDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs/"+deniedExecution.ActiveRun.ID+"/delegations", "", http.StatusOK, &denied)
	if len(denied) != 0 {
		t.Fatalf("disabled parent created lineage: %+v", denied)
	}
}

func admitDelegationExecutionRun(t *testing.T, ctx context.Context, database *postgres.Store, runID, owner string) *store.SchedulerAdmission {
	t.Helper()
	admission, err := database.AdmitNextJob(ctx, owner, time.Minute, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if admission == nil || admission.Run.ID != runID || admission.Run.Status != "STARTING" {
		t.Fatalf("scheduler admission=%+v want run %s STARTING", admission, runID)
	}
	running, err := database.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: admission.Run.ProjectID,
		JobID: admission.Job.ID,
		RunID: admission.Run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus: "RUNNING",
	})
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != "RUNNING" {
		t.Fatalf("running transition=%+v", running)
	}
	return admission
}

func completeDelegationExecutionRun(t *testing.T, ctx context.Context, database *postgres.Store, admission *store.SchedulerAdmission, status string) {
	t.Helper()
	if admission == nil {
		t.Fatal("scheduler admission is required")
	}
	completed, err := database.TransitionAdmittedJob(ctx, store.SchedulerTransition{
		ProjectID: admission.Run.ProjectID,
		JobID: admission.Job.ID,
		RunID: admission.Run.ID,
		LeaseToken: admission.Lease.LeaseToken,
		RunStatus: status,
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != status {
		t.Fatalf("terminal transition=%+v want status %s", completed, status)
	}
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
