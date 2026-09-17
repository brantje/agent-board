package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const delegationSelectColumns = `
	id::text,
	project_id::text,
	issue_id::text,
	parent_run_id::text,
	parent_agent_id::text,
	target_agent_id::text,
	task,
	delegated_run_id::text,
	request_key,
	created_at,
	updated_at
`

func (s *Store) RequestDelegation(ctx context.Context, input store.RequestDelegationCommand) (store.RequestDelegationResult, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.ParentRunID = strings.TrimSpace(input.ParentRunID)
	input.TargetAgentID = strings.TrimSpace(input.TargetAgentID)
	input.Task = strings.TrimSpace(input.Task)
	input.RequestKey = strings.TrimSpace(input.RequestKey)
	if input.ProjectID == "" || input.ParentRunID == "" || input.TargetAgentID == "" || input.Task == "" || input.RequestKey == "" || utf8.RuneCountInString(input.Task) > store.MaxDelegationTaskCharacters {
		return store.RequestDelegationResult{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.RequestDelegationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if result, found, err := existingDelegationRequest(ctx, tx, input); err != nil {
		return store.RequestDelegationResult{}, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return store.RequestDelegationResult{}, err
		}
		return result, nil
	}

	parent, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, input.ProjectID, input.ParentRunID))
	if err != nil {
		return store.RequestDelegationResult{}, err
	}

	// A concurrent retry can have committed while this transaction waited for
	// the parent Run lock. Re-check under the lock before applying authority rules.
	if result, found, err := existingDelegationRequest(ctx, tx, input); err != nil {
		return store.RequestDelegationResult{}, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return store.RequestDelegationResult{}, err
		}
		return result, nil
	}

	if parent.Status != "RUNNING" || parent.AgentID == nil || *parent.AgentID == input.TargetAgentID {
		return store.RequestDelegationResult{}, store.ErrConflict
	}
	var delegatedParent bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM delegations WHERE project_id=$1 AND delegated_run_id=$2)`, input.ProjectID, parent.ID).Scan(&delegatedParent); err != nil {
		return store.RequestDelegationResult{}, err
	}
	if delegatedParent {
		return store.RequestDelegationResult{}, store.ErrConflict
	}

	var allowDelegation bool
	if err := tx.QueryRow(ctx, `
		SELECT allow_delegation
		FROM agents
		WHERE id=$2 AND (project_id IS NULL OR project_id=$1)
		FOR SHARE
	`, input.ProjectID, *parent.AgentID).Scan(&allowDelegation); err != nil {
		return store.RequestDelegationResult{}, notFound(err)
	}
	if !allowDelegation {
		return store.RequestDelegationResult{}, store.ErrConflict
	}

	issue, _, _, err := lockAssignmentIssue(ctx, tx, input.ProjectID, parent.IssueID)
	if err != nil {
		return store.RequestDelegationResult{}, err
	}
	if issue.ID != parent.IssueID {
		return store.RequestDelegationResult{}, store.ErrConflict
	}
	if err := s.verifyRunnableAgent(ctx, tx, input.ProjectID, input.TargetAgentID); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
			return store.RequestDelegationResult{}, store.ErrConflict
		}
		return store.RequestDelegationResult{}, err
	}
	if _, err := activeRunForAgent(ctx, tx, input.ProjectID, parent.IssueID, input.TargetAgentID); err == nil {
		return store.RequestDelegationResult{}, store.ErrConflict
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.RequestDelegationResult{}, err
	}

	attempt, err := nextIssueRunAttempt(ctx, tx, input.ProjectID, parent.IssueID)
	if err != nil {
		return store.RequestDelegationResult{}, err
	}
	run, job, event, err := createQueuedRunTx(ctx, tx, queuedRunInput{
		ProjectID:      input.ProjectID,
		IssueID:        parent.IssueID,
		WorkspaceID:    parent.WorkspaceID,
		AgentID:        input.TargetAgentID,
		Attempt:        attempt,
		IdempotencyKey: delegationSchedulerKey(parent.ID, input.RequestKey),
	})
	if err != nil {
		return store.RequestDelegationResult{}, err
	}
	run, job, err = holdDelegatedRunForWorkspaceHandoff(ctx, tx, run, job)
	if err != nil {
		return store.RequestDelegationResult{}, err
	}
	delegation, err := scanDelegation(tx.QueryRow(ctx, `
		INSERT INTO delegations (project_id, issue_id, parent_run_id, parent_agent_id, target_agent_id, task, delegated_run_id, request_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+delegationSelectColumns,
		input.ProjectID, parent.IssueID, parent.ID, *parent.AgentID, input.TargetAgentID, input.Task, run.ID, input.RequestKey,
	))
	if err != nil {
		return store.RequestDelegationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.RequestDelegationResult{}, err
	}
	return store.RequestDelegationResult{Delegation: delegation, DelegatedRun: run, SchedulerJob: job, Events: []store.Event{event}}, nil
}

func holdDelegatedRunForWorkspaceHandoff(ctx context.Context, tx pgx.Tx, run store.Run, job store.SchedulerJob) (store.Run, store.SchedulerJob, error) {
	var err error
	run, err = scanRun(tx.QueryRow(ctx, `
		UPDATE runs
		SET queue_reason=$3, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='QUEUED'
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		          status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, run.ProjectID, run.ID, store.DelegationWorkspaceHandoffWaitReason))
	if err != nil {
		return store.Run{}, store.SchedulerJob{}, err
	}
	job, err = scanSchedulerJob(tx.QueryRow(ctx, `
		UPDATE scheduler_jobs
		SET wait_reason=$4,
		    available_at='9999-12-31 23:59:59+00'::timestamptz,
		    updated_at=now()
		WHERE project_id=$1 AND id=$2 AND run_id=$3 AND state='QUEUED'
		RETURNING id::text, project_id::text, run_id::text, kind, state, wait_reason,
		          idempotency_key, available_at, created_at, updated_at
	`, job.ProjectID, job.ID, job.RunID, store.DelegationWorkspaceHandoffWaitReason))
	if err != nil {
		return store.Run{}, store.SchedulerJob{}, err
	}
	return run, job, nil
}

// CompleteDelegationWorkspaceHandoff makes the already-created delegated START
// job eligible for normal scheduler admission. The parent is still RUNNING at
// this point, so the existing shared-Workspace admission fence continues to
// prevent the delegate from starting until the parent actually yields.
func (s *Store) CompleteDelegationWorkspaceHandoff(ctx context.Context, projectID, parentRunID, delegationID, delegatedRunID string) error {
	projectID = strings.TrimSpace(projectID)
	parentRunID = strings.TrimSpace(parentRunID)
	delegationID = strings.TrimSpace(delegationID)
	delegatedRunID = strings.TrimSpace(delegatedRunID)
	if projectID == "" || parentRunID == "" || delegationID == "" || delegatedRunID == "" {
		return store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	delegation, err := scanDelegation(tx.QueryRow(ctx, `
		SELECT `+delegationSelectColumns+`
		FROM delegations
		WHERE project_id=$1 AND id=$2 AND parent_run_id=$3 AND delegated_run_id=$4
		FOR UPDATE
	`, projectID, delegationID, parentRunID, delegatedRunID))
	if err != nil {
		return err
	}
	parent, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs WHERE project_id=$1 AND id=$2 FOR UPDATE
	`, projectID, parentRunID))
	if err != nil {
		return err
	}
	delegated, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs WHERE project_id=$1 AND id=$2 FOR UPDATE
	`, projectID, delegatedRunID))
	if err != nil {
		return err
	}
	if parent.IssueID != delegation.IssueID || delegated.IssueID != delegation.IssueID || parent.WorkspaceID != delegated.WorkspaceID {
		return store.ErrConflict
	}

	job, err := scanSchedulerJob(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, run_id::text, kind, state, wait_reason,
		       idempotency_key, available_at, created_at, updated_at
		FROM scheduler_jobs
		WHERE project_id=$1 AND run_id=$2 AND kind='START'
		ORDER BY created_at, id
		LIMIT 1
		FOR UPDATE
	`, projectID, delegatedRunID))
	if err != nil {
		return err
	}
	if job.WaitReason == nil || *job.WaitReason != store.DelegationWorkspaceHandoffWaitReason {
		return tx.Commit(ctx)
	}
	if parent.Status != "RUNNING" || delegated.Status != "QUEUED" || job.State != "QUEUED" {
		return store.ErrConflict
	}

	if _, err := tx.Exec(ctx, `
		UPDATE scheduler_jobs
		SET wait_reason=NULL, available_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2 AND run_id=$3 AND state='QUEUED' AND wait_reason=$4
	`, projectID, job.ID, delegatedRunID, store.DelegationWorkspaceHandoffWaitReason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE runs
		SET queue_reason=NULL, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='QUEUED' AND queue_reason=$3
	`, projectID, delegatedRunID, store.DelegationWorkspaceHandoffWaitReason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func existingDelegationRequest(ctx context.Context, tx pgx.Tx, input store.RequestDelegationCommand) (store.RequestDelegationResult, bool, error) {
	delegation, err := scanDelegation(tx.QueryRow(ctx, `
		SELECT `+delegationSelectColumns+`
		FROM delegations
		WHERE project_id=$1 AND parent_run_id=$2 AND request_key=$3
	`, input.ProjectID, input.ParentRunID, input.RequestKey))
	if errors.Is(err, store.ErrNotFound) {
		return store.RequestDelegationResult{}, false, nil
	}
	if err != nil {
		return store.RequestDelegationResult{}, false, err
	}
	if delegation.TargetAgentID != input.TargetAgentID || delegation.Task != input.Task {
		return store.RequestDelegationResult{}, false, store.ErrConflict
	}
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs WHERE project_id=$1 AND id=$2
	`, input.ProjectID, delegation.DelegatedRunID))
	if err != nil {
		return store.RequestDelegationResult{}, false, err
	}
	job, err := scanSchedulerJob(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, run_id::text, kind, state, wait_reason,
		       idempotency_key, available_at, created_at, updated_at
		FROM scheduler_jobs
		WHERE project_id=$1 AND run_id=$2 AND kind='START'
		ORDER BY created_at, id
		LIMIT 1
	`, input.ProjectID, delegation.DelegatedRunID))
	if err != nil {
		return store.RequestDelegationResult{}, false, err
	}
	return store.RequestDelegationResult{Delegation: delegation, DelegatedRun: run, SchedulerJob: job}, true, nil
}

func (s *Store) GetDelegationByRun(ctx context.Context, projectID, runID string) (store.Delegation, error) {
	return scanDelegation(s.pool.QueryRow(ctx, `
		SELECT `+delegationSelectColumns+`
		FROM delegations
		WHERE project_id=$1 AND delegated_run_id=$2
	`, projectID, runID))
}

func (s *Store) ListDelegationsByParentRun(ctx context.Context, projectID, parentRunID string) ([]store.Delegation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+delegationSelectColumns+`
		FROM delegations
		WHERE project_id=$1 AND parent_run_id=$2
		ORDER BY created_at, id
	`, projectID, parentRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]store.Delegation, 0)
	for rows.Next() {
		value, err := scanDelegation(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func scanDelegation(row pgx.Row) (store.Delegation, error) {
	var value store.Delegation
	err := row.Scan(
		&value.ID,
		&value.ProjectID,
		&value.IssueID,
		&value.ParentRunID,
		&value.ParentAgentID,
		&value.TargetAgentID,
		&value.Task,
		&value.DelegatedRunID,
		&value.RequestKey,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	if err != nil {
		return store.Delegation{}, notFound(err)
	}
	return value, nil
}

func delegationSchedulerKey(parentRunID, requestKey string) string {
	digest := sha256.Sum256([]byte(parentRunID + "\x00" + requestKey))
	return fmt.Sprintf("delegation:%x:start", digest[:])
}
