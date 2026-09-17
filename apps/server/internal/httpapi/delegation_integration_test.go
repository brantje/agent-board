package httpapi

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
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/jackc/pgx/v5"
)

func TestDelegationPublicAPIEndToEnd(t *testing.T) {
	baseDatabaseURL := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_DATABASE_URL"))
	if baseDatabaseURL == "" {
		t.Skip("AGENT_BOARD_TEST_DATABASE_URL is required for delegation API E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	databaseURL := isolatedDelegationAPIDatabase(t, ctx, baseDatabaseURL)
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	router := NewRouter(app.New(database))
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	prefix := fmt.Sprintf("D%08X", uint32(time.Now().UnixNano()))
	var project ProjectDTO
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects", fmt.Sprintf(`{"name":"Delegation API E2E","issuePrefix":"%s","repositoryPath":%q,"defaultBranch":"main","workflowSettings":{}}`, prefix, t.TempDir()), http.StatusCreated, &project)

	var provider ProviderDTO
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/providers", `{"name":"Provider","kind":"test","enabled":true,"safeMetadata":{}}`, http.StatusCreated, &provider)
	var model ModelProfileDTO
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/model-profiles", fmt.Sprintf(`{"providerId":"%s","name":"Model","model":"test","generationSettings":{},"enabled":true}`, provider.ID), http.StatusCreated, &model)

	var parent AgentDTO
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/agents", fmt.Sprintf(`{"name":"Parent","roleInstructions":"delegate bounded work","engine":"scripted","modelProfileId":"%s","engineSettings":{},"concurrencyLimit":1,"allowDelegation":true,"state":"ENABLED"}`, model.ID), http.StatusCreated, &parent)
	if !parent.AllowDelegation {
		t.Fatal("public Agent create did not persist allowDelegation")
	}
	var target AgentDTO
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/agents", fmt.Sprintf(`{"name":"Target","roleInstructions":"perform bounded work","engine":"scripted","modelProfileId":"%s","engineSettings":{},"concurrencyLimit":1,"allowDelegation":false,"state":"ENABLED"}`, model.ID), http.StatusCreated, &target)

	var issue IssueDTO
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues", `{"title":"Delegate through API","description":"bounded delegation","status":"TODO"}`, http.StatusCreated, &issue)
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues/"+issue.ID+"/assignment", fmt.Sprintf(`{"assignedTo":{"type":"AGENT","id":"%s"}}`, parent.ID), http.StatusOK, nil)
	var execution IssueExecutionStateDTO
	delegationAPIJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID+"/execution", "", http.StatusOK, &execution)
	if execution.ActiveRun == nil || execution.ActiveRun.AgentID == nil || *execution.ActiveRun.AgentID != parent.ID {
		t.Fatalf("API assignment did not create authoritative parent Run: %+v", execution)
	}
	parentRun := *execution.ActiveRun
	markDelegationAPIParentRunning(t, ctx, conn, project.ID, parentRun.ID)

	var before IssueDTO
	delegationAPIJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID, "", http.StatusOK, &before)
	requestBody := fmt.Sprintf(`{"targetAgentId":"%s","task":"inspect scheduler ownership","requestKey":"api-e2e-request-1"}`, target.ID)
	var created DelegationDTO
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/runs/"+parentRun.ID+"/delegations", requestBody, http.StatusCreated, &created)
	if created.ParentRunID != parentRun.ID || created.ParentAgentID != parent.ID || created.TargetAgentID != target.ID || created.IssueID != issue.ID {
		t.Fatalf("created delegation=%+v", created)
	}

	var replay DelegationDTO
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/runs/"+parentRun.ID+"/delegations", requestBody, http.StatusCreated, &replay)
	if replay.ID != created.ID || replay.DelegatedRunID != created.DelegatedRunID {
		t.Fatalf("idempotent replay created different lineage: first=%+v replay=%+v", created, replay)
	}
	var listed []DelegationDTO
	delegationAPIJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs/"+parentRun.ID+"/delegations", "", http.StatusOK, &listed)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("parent delegation list=%+v", listed)
	}
	var childLineage DelegationDTO
	delegationAPIJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs/"+created.DelegatedRunID+"/delegation", "", http.StatusOK, &childLineage)
	if childLineage.ParentRunID != parentRun.ID || childLineage.DelegatedRunID != created.DelegatedRunID {
		t.Fatalf("child lineage=%+v", childLineage)
	}

	var runs []RunDTO
	delegationAPIJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs", "", http.StatusOK, &runs)
	children := 0
	for _, run := range runs {
		if run.ID == created.DelegatedRunID {
			children++
			if run.AgentID == nil || *run.AgentID != target.ID || run.IssueID != issue.ID || run.WorkspaceID != parentRun.WorkspaceID || run.Status != "QUEUED" {
				t.Fatalf("ordinary delegated Run=%+v", run)
			}
		}
	}
	if children != 1 {
		t.Fatalf("delegated Run count=%d want 1", children)
	}
	var startJobs int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM scheduler_jobs WHERE project_id=$1 AND run_id=$2 AND kind='START'`, project.ID, created.DelegatedRunID).Scan(&startJobs); err != nil {
		t.Fatal(err)
	}
	if startJobs != 1 {
		t.Fatalf("delegated START scheduler jobs=%d want 1", startJobs)
	}
	var after IssueDTO
	delegationAPIJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID, "", http.StatusOK, &after)
	if after.Status != before.Status || after.AssignedTo == nil || before.AssignedTo == nil || *after.AssignedTo != *before.AssignedTo {
		t.Fatalf("delegation changed Issue authority: before=%+v after=%+v", before, after)
	}

	// Negative public path: turn the parent policy off, create another API-owned
	// parent execution, and prove no child lineage/Run is accepted.
	parent.AllowDelegation = false
	delegationAPIJSON(t, router, http.MethodPut, "/api/projects/"+project.ID+"/agents/"+parent.ID, fmt.Sprintf(`{"name":%q,"roleInstructions":%q,"engine":%q,"modelProfileId":"%s","engineSettings":{},"concurrencyLimit":%d,"allowDelegation":false,"state":%q}`, parent.Name, parent.RoleInstructions, parent.Engine, parent.ModelProfileID, parent.ConcurrencyLimit, parent.State), http.StatusOK, &parent)
	var deniedIssue IssueDTO
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues", `{"title":"Disabled delegation","description":"must reject","status":"TODO"}`, http.StatusCreated, &deniedIssue)
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID+"/assignment", fmt.Sprintf(`{"assignedTo":{"type":"AGENT","id":"%s"}}`, parent.ID), http.StatusOK, nil)
	var deniedExecution IssueExecutionStateDTO
	delegationAPIJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID+"/execution", "", http.StatusOK, &deniedExecution)
	if deniedExecution.ActiveRun == nil {
		t.Fatal("disabled-policy fixture has no parent Run")
	}
	markDelegationAPIParentRunning(t, ctx, conn, project.ID, deniedExecution.ActiveRun.ID)
	delegationAPIJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/runs/"+deniedExecution.ActiveRun.ID+"/delegations", fmt.Sprintf(`{"targetAgentId":"%s","task":"must reject","requestKey":"disabled"}`, target.ID), http.StatusConflict, nil)
	var deniedCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM delegations WHERE project_id=$1 AND parent_run_id=$2`, project.ID, deniedExecution.ActiveRun.ID).Scan(&deniedCount); err != nil {
		t.Fatal(err)
	}
	if deniedCount != 0 {
		t.Fatalf("disabled parent created %d delegation rows", deniedCount)
	}
}

func isolatedDelegationAPIDatabase(t *testing.T, ctx context.Context, baseDatabaseURL string) string {
	t.Helper()
	parsed, err := url.Parse(baseDatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("agent_board_delegation_%x", uint64(time.Now().UnixNano()))
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
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cleanup, connectErr := pgx.Connect(cleanupCtx, baseDatabaseURL)
		if connectErr != nil {
			t.Errorf("connect to drop delegation API database: %v", connectErr)
			return
		}
		defer cleanup.Close(cleanupCtx)
		if _, dropErr := cleanup.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+databaseName+" WITH (FORCE)"); dropErr != nil {
			t.Errorf("drop delegation API database: %v", dropErr)
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
		t.Fatalf("apply isolated delegation API schema: %v", err)
	}
	return isolatedURL
}

func markDelegationAPIParentRunning(t *testing.T, ctx context.Context, conn *pgx.Conn, projectID, runID string) {
	t.Helper()
	result, err := conn.Exec(ctx, `UPDATE runs SET status='RUNNING', started_at=COALESCE(started_at, now()), updated_at=now() WHERE project_id=$1 AND id=$2 AND status='QUEUED'`, projectID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("advance API-created parent Run %s to RUNNING affected %d rows", runID, result.RowsAffected())
	}
}

func delegationAPIJSON(t *testing.T, router http.Handler, method, path, body string, wantStatus int, target any) {
	t.Helper()
	reader := strings.NewReader(body)
	request := httptest.NewRequest(method, path, reader)
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
