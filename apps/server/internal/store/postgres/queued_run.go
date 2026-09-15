package postgres

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

type queuedRunInput struct {
	ProjectID     string
	IssueID       string
	WorkspaceID   string
	AgentID       string
	Attempt       int
	IdempotencyKey string
}

func createQueuedRunTx(ctx context.Context, tx pgx.Tx, input queuedRunInput) (store.Run, store.SchedulerJob, store.Event, error) {
	run, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO runs (project_id, issue_id, workspace_id, agent_id, attempt, status)
		VALUES ($1, $2, $3, $4, $5, 'QUEUED')
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		          status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, input.ProjectID, input.IssueID, input.WorkspaceID, input.AgentID, input.Attempt))
	if err != nil {
		return store.Run{}, store.SchedulerJob{}, store.Event{}, err
	}
	key := input.IdempotencyKey
	if key == "" {
		key = "run:" + run.ID + ":start"
	}
	job, err := scanSchedulerJob(tx.QueryRow(ctx, `
		INSERT INTO scheduler_jobs (project_id, run_id, kind, state, idempotency_key)
		VALUES ($1, $2, 'START', 'QUEUED', $3)
		RETURNING id::text, project_id::text, run_id::text, kind, state, wait_reason,
		          idempotency_key, available_at, created_at, updated_at
	`, input.ProjectID, run.ID, key))
	if err != nil {
		return store.Run{}, store.SchedulerJob{}, store.Event{}, err
	}
	payload, _ := json.Marshal(map[string]any{"status": run.Status, "attempt": run.Attempt})
	issueID, runID, agentID, workspaceID := input.IssueID, run.ID, input.AgentID, input.WorkspaceID
	event, err := appendEventTx(ctx, tx, store.Event{
		Type: "run.created", ProjectID: input.ProjectID, IssueID: &issueID, RunID: &runID,
		AgentID: &agentID, WorkspaceID: &workspaceID, Actor: store.EmptyObject, Payload: payload,
	})
	if err != nil {
		return store.Run{}, store.SchedulerJob{}, store.Event{}, err
	}
	return run, job, event, nil
}
