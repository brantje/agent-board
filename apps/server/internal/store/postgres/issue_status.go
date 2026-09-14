package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) SetIssueStatus(ctx context.Context, input store.IssueStatusMutation) (store.IssueMutationResult, error) {
	if !store.ValidIssueStatus(input.Status) {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	if input.RunID == nil && (input.AgentID != nil || input.WorkspaceID != nil) {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	if input.RunID != nil && (input.AgentID == nil || input.WorkspaceID == nil) {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	if input.Recovery && input.RunID == nil {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	run, err := lockIssueStatusRunFence(ctx, tx, input)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	previous, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, input.ProjectID, input.IssueID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if input.Recovery {
		if err := validateIssueStatusRecoveryFence(ctx, tx, input, run); err != nil {
			return store.IssueMutationResult{}, err
		}
	}
	if previous.Status == input.Status {
		if err := tx.Commit(ctx); err != nil {
			return store.IssueMutationResult{}, err
		}
		return store.IssueMutationResult{Issue: previous}, nil
	}

	updated := previous
	updated.Status = input.Status
	result, err := applyIssueMutationTx(ctx, tx, issueMutationTxInput{
		Issue:          updated,
		Previous:       previous,
		RepositoryPath: repositoryPath,
		DefaultBranch:  defaultBranch,
		Actor:          input.Actor,
		RunID:          input.RunID,
		AgentID:        input.AgentID,
		WorkspaceID:    input.WorkspaceID,
	})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.IssueMutationResult{}, err
	}
	return result, nil
}

func lockIssueStatusRunFence(ctx context.Context, tx pgx.Tx, input store.IssueStatusMutation) (store.Run, error) {
	if input.RunID == nil {
		return store.Run{}, nil
	}
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, input.ProjectID, *input.RunID))
	if err != nil {
		return store.Run{}, notFound(err)
	}
	if run.Status != "RUNNING" || run.IssueID != input.IssueID || run.WorkspaceID != *input.WorkspaceID ||
		run.AgentID == nil || *run.AgentID != *input.AgentID {
		return store.Run{}, store.ErrConflict
	}
	return run, nil
}

func validateIssueStatusRecoveryFence(ctx context.Context, tx pgx.Tx, input store.IssueStatusMutation, run store.Run) error {
	if input.RunID == nil || run.StartedAt == nil {
		return store.ErrConflict
	}
	var eventRunID string
	var actorType string
	var eventCreatedAt time.Time
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(run_id::text, ''), COALESCE(actor->>'type', ''), created_at
		FROM events
		WHERE project_id=$1 AND issue_id=$2
		  AND type IN ('issue.updated', 'issue.status_changed')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, input.ProjectID, input.IssueID).Scan(&eventRunID, &actorType, &eventCreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !eventCreatedAt.After(*run.StartedAt) {
		return nil
	}
	if eventRunID == *input.RunID && actorType == store.ActorTypeAgent {
		return nil
	}
	return store.ErrConflict
}
