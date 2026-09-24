package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func finalizeDelegatedRunTx(ctx context.Context, tx pgx.Tx, child store.Run) ([]store.Event, error) {
	if child.Status != "COMPLETED" && child.Status != "FAILED" && child.Status != "CANCELLED" {
		return nil, nil
	}
	// Closing the execution request is independent from delegation lineage:
	// ordinary queued Runs can also carry coalesced comment work. Keep this
	// in the canonical terminal transaction so cancellation/failure/restart
	// paths cannot strand an open coalescing bucket.
	if err := closeAgentWorkRequestsForRunTx(ctx, tx, child.ProjectID, child.ID); err != nil {
		return nil, err
	}
	delegation, err := scanDelegation(tx.QueryRow(ctx, `
		SELECT `+delegationSelectColumns+`
		FROM delegations
		WHERE project_id=$1 AND delegated_run_id=$2
		FOR UPDATE
	`, child.ProjectID, child.ID))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if delegation.Outcome != nil {
		return nil, nil
	}
	if delegation.IssueID != child.IssueID || child.AgentID == nil || *child.AgentID != delegation.TargetAgentID {
		return nil, store.ErrConflict
	}

	outcome, eventType, fallback, err := delegationTerminalOutcome(child.Status)
	if err != nil {
		return nil, err
	}
	summary, resultEventID, err := delegatedResultEvidence(ctx, tx, child, fallback)
	if err != nil {
		return nil, err
	}
	accepted, err := delegatedWorkspaceAccepted(ctx, tx, child)
	if err != nil {
		return nil, err
	}

	var parent *store.Run
	var continuationJobID *string
	if delegation.ParentRunID != "" {
		parentValue, err := scanRun(tx.QueryRow(ctx, `
			SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
			       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
			FROM runs
			WHERE project_id=$1 AND id=$2
			FOR UPDATE
		`, delegation.ProjectID, delegation.ParentRunID))
		if err != nil {
			return nil, err
		}
		if parentValue.IssueID != child.IssueID || parentValue.WorkspaceID != child.WorkspaceID || parentValue.AgentID == nil || *parentValue.AgentID != delegation.ParentAgentID {
			return nil, store.ErrConflict
		}
		parent = &parentValue

		switch parentValue.Status {
		case "PAUSED":
			parentValue, err = scanRun(tx.QueryRow(ctx, `
				UPDATE runs
				SET status='QUEUED', queue_reason=NULL, failure_reason=NULL, completed_at=NULL, updated_at=now()
				WHERE project_id=$1 AND id=$2 AND status='PAUSED'
				RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
				          status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
			`, parentValue.ProjectID, parentValue.ID))
			if err != nil {
				return nil, err
			}
			parent = &parentValue
			job, err := ensureDelegationResumeJobTx(ctx, tx, delegation, parentValue)
			if err != nil {
				return nil, err
			}
			continuationJobID = &job.ID
		case "QUEUED":
			job, err := ensureDelegationResumeJobTx(ctx, tx, delegation, parentValue)
			if err != nil {
				return nil, err
			}
			continuationJobID = &job.ID
		case "COMPLETED", "FAILED", "CANCELLED":
			// A terminal parent remains authoritative. Persist the child outcome, but
			// never resurrect parent execution with a continuation job.
		default:
			return nil, store.ErrConflict
		}
	} else if delegation.SourceCommentID == nil || delegation.ParentAgentID != "" {
		return nil, store.ErrConflict
	}

	updated, err := scanDelegation(tx.QueryRow(ctx, `
		UPDATE delegations
		SET outcome=$3,
		    result_summary=$4,
		    result_event_id=$5,
		    workspace_changes_accepted=$6,
		    continuation_job_id=$7,
		    completed_at=now(),
		    updated_at=now()
		WHERE project_id=$1 AND id=$2 AND outcome IS NULL
		RETURNING `+delegationSelectColumns,
		delegation.ProjectID, delegation.ID, outcome, summary, resultEventID, accepted, continuationJobID,
	))
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(map[string]any{
		"delegationId":             updated.ID,
		"delegatedRunId":           updated.DelegatedRunID,
		"outcome":                  outcome,
		"resultEventId":            resultEventID,
		"workspaceChangesAccepted": accepted,
		"continuationJobId":        continuationJobID,
	})
	if err != nil {
		return nil, err
	}
	issueID, runID, workspaceID := updated.IssueID, child.ID, child.WorkspaceID
	agentID := child.AgentID
	if parent != nil {
		runID = parent.ID
		workspaceID = parent.WorkspaceID
		agentID = parent.AgentID
	}
	event, err := appendEventTx(ctx, tx, store.Event{
		Type:        eventType,
		ProjectID:   updated.ProjectID,
		IssueID:     &issueID,
		RunID:       &runID,
		AgentID:     agentID,
		WorkspaceID: &workspaceID,
		Actor:       store.EmptyObject,
		Payload:     payload,
	})
	if err != nil {
		return nil, err
	}
	return []store.Event{event}, nil
}

func delegationTerminalOutcome(status string) (outcome, eventType, fallback string, err error) {
	switch status {
	case "COMPLETED":
		return store.DelegationOutcomeSucceeded, "delegation.completed", "Delegated task completed successfully.", nil
	case "FAILED":
		return store.DelegationOutcomeFailed, "delegation.failed", "Delegated task failed.", nil
	case "CANCELLED":
		return store.DelegationOutcomeCancelled, "delegation.cancelled", "Delegated task was cancelled.", nil
	default:
		return "", "", "", store.ErrInvalidArgument
	}
}

func delegatedResultEvidence(ctx context.Context, tx pgx.Tx, child store.Run, fallback string) (string, *string, error) {
	var eventID, summary string
	err := tx.QueryRow(ctx, `
		SELECT id::text,
		       CASE
		         WHEN type='agent.message' THEN COALESCE(payload->>'message', '')
		         WHEN type='run.failed' THEN COALESCE(payload->>'reason', '')
		         ELSE ''
		       END
		FROM events
		WHERE project_id=$1
		  AND run_id=$2
		  AND (
			($3='FAILED' AND type='run.failed')
			OR (
				$3<>'FAILED'
				AND type='agent.message'
				AND COALESCE(NULLIF(BTRIM(payload->>'kind'), ''), 'message')='message'
			)
		  )
		ORDER BY sequence DESC
		LIMIT 1
	`, child.ProjectID, child.ID, child.Status).Scan(&eventID, &summary)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", nil, err
	}
	if strings.TrimSpace(summary) == "" {
		eventID = ""
	}
	if strings.TrimSpace(summary) == "" && child.Status == "FAILED" && child.FailureReason != nil {
		summary = *child.FailureReason
	}
	if strings.TrimSpace(summary) == "" {
		summary = fallback
	}
	summary = boundDelegationResult(summary)
	if eventID == "" {
		return summary, nil, nil
	}
	return summary, &eventID, nil
}

func delegatedWorkspaceAccepted(ctx context.Context, tx pgx.Tx, child store.Run) (bool, error) {
	var accepted bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM events
			WHERE project_id=$1
			  AND run_id=$2
			  AND (
				type='delegation.workspace_accepted'
				OR (
					type='workspace.transfer.completed'
					AND payload->>'direction' IN ('from_runner', 'git_publish')
				)
			  )
		)
	`, child.ProjectID, child.ID).Scan(&accepted)
	return accepted, err
}

func ensureDelegationResumeJobTx(ctx context.Context, tx pgx.Tx, delegation store.Delegation, parent store.Run) (store.SchedulerJob, error) {
	key := "delegation:" + delegation.ID + ":resume"
	job, err := scanSchedulerJob(tx.QueryRow(ctx, `
		INSERT INTO scheduler_jobs (project_id, run_id, kind, state, idempotency_key, available_at)
		VALUES ($1, $2, 'RESUME', 'QUEUED', $3, now())
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id::text, project_id::text, run_id::text, kind, state, wait_reason,
		          idempotency_key, available_at, created_at, updated_at
	`, parent.ProjectID, parent.ID, key))
	if err == nil {
		return job, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.SchedulerJob{}, err
	}
	job, err = scanSchedulerJob(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, run_id::text, kind, state, wait_reason,
		       idempotency_key, available_at, created_at, updated_at
		FROM scheduler_jobs
		WHERE idempotency_key=$1
		FOR UPDATE
	`, key))
	if err != nil {
		return store.SchedulerJob{}, err
	}
	if job.ProjectID != parent.ProjectID || job.RunID != parent.ID || job.Kind != "RESUME" || job.State != "QUEUED" {
		return store.SchedulerJob{}, store.ErrConflict
	}
	return job, nil
}

func boundDelegationResult(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > store.MaxDelegationResultCharacters {
		value = string(runes[:store.MaxDelegationResultCharacters])
	}
	if value == "" {
		return "Delegated task produced no summary."
	}
	return value
}
