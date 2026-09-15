package postgres

import (
	"context"

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
	result, err := s.applyIssueMutationTx(ctx, tx, issueMutationTxInput{
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
		return store.Run{}, err
	}
	if run.IssueID != input.IssueID || run.AgentID == nil || run.WorkspaceID == "" ||
		*run.AgentID != *input.AgentID || run.WorkspaceID != *input.WorkspaceID {
		return store.Run{}, store.ErrConflict
	}
	return run, nil
}

func validateIssueStatusRecoveryFence(ctx context.Context, tx pgx.Tx, input store.IssueStatusMutation, run store.Run) error {
	if run.Status != "RUNNING" && run.Status != "WAITING_FOR_INPUT" {
		return store.ErrConflict
	}
	var activeRunID string
	err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM runs
		WHERE project_id=$1 AND issue_id=$2 AND agent_id=$3 AND status = ANY($4::text[])
		ORDER BY attempt DESC
		LIMIT 1
	`, input.ProjectID, input.IssueID, *input.AgentID, activeRunStatuses).Scan(&activeRunID)
	if err != nil {
		return notFound(err)
	}
	if activeRunID != *input.RunID {
		return store.ErrConflict
	}
	return nil
}
