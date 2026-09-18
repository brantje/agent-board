package runexec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode"
	"github.com/brantje/agent-board/apps/server/internal/httpapi"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
)

// assertDelegatedWorkspaceFeedsLaterNormalRun proves the returned delegated
// revision through the ordinary Issue execution path. The test deliberately
// creates a later non-delegated Agent assignment, lets the scheduler admit its
// normal START job, and relies on Processor/Runner workspace transfer. The
// delegationExecutionRunnerClient rejects the execution at Start if the inbound
// transfer does not already contain the delegated change.
func assertDelegatedWorkspaceFeedsLaterNormalRun(
	t *testing.T,
	ctx context.Context,
	router http.Handler,
	database *postgres.Store,
	projectID string,
	issueID string,
	workspaceID string,
	modelProfileID string,
	runnerClient *delegationExecutionRunnerClient,
) {
	t.Helper()

	var continuation httpapi.AgentDTO
	delegationExecutionJSON(
		t,
		router,
		http.MethodPost,
		"/api/projects/"+projectID+"/agents",
		fmt.Sprintf(`{"name":"Ordinary continuation","roleInstructions":"Inspect the current Workspace and finish normally without delegation.","engine":%q,"modelProfileId":"%s","engineSettings":{},"concurrencyLimit":1,"allowDelegation":false,"state":"ENABLED"}`, opencode.Name, modelProfileID),
		http.StatusCreated,
		&continuation,
	)

	delegationExecutionJSON(
		t,
		router,
		http.MethodPost,
		"/api/projects/"+projectID+"/issues/"+issueID+"/assignment",
		fmt.Sprintf(`{"assignedTo":{"type":"AGENT","id":"%s"}}`, continuation.ID),
		http.StatusOK,
		nil,
	)

	var execution httpapi.IssueExecutionStateDTO
	delegationExecutionJSON(
		t,
		router,
		http.MethodGet,
		"/api/projects/"+projectID+"/issues/"+issueID+"/execution",
		"",
		http.StatusOK,
		&execution,
	)
	if execution.ActiveRun == nil || execution.ActiveRun.AgentID == nil || *execution.ActiveRun.AgentID != continuation.ID {
		t.Fatalf("ordinary continuation was not created through Issue execution: %+v", execution)
	}
	if execution.ActiveRun.WorkspaceID != workspaceID {
		t.Fatalf("ordinary continuation workspace=%s want inherited Issue workspace %s", execution.ActiveRun.WorkspaceID, workspaceID)
	}

	terminal := waitForDelegationExecutionRun(t, ctx, router, projectID, execution.ActiveRun.ID)
	if terminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("ordinary continuation status=%s failure=%v want READY_FOR_REVIEW", terminal.Status, terminal.FailureReason)
	}
	if terminal.WorkspaceID != workspaceID {
		t.Fatalf("ordinary continuation terminal workspace=%s want %s", terminal.WorkspaceID, workspaceID)
	}
	if _, err := database.GetDelegationByRun(ctx, projectID, terminal.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ordinary continuation unexpectedly has delegation lineage: %v", err)
	}

	_, _, continuationStarts, _, _ := runnerClient.stats()
	if continuationStarts != 2 {
		t.Fatalf("continuation executions that observed delegated state=%d want 2 (parent resume plus later ordinary Run)", continuationStarts)
	}
}
