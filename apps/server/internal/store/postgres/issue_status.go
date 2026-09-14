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

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockIssueStatusRunFence(ctx, tx, input); err != nil {
		return store.IssueMutationResult{}, err
	}
	previous, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, input.ProjectID, input.IssueID)
	if err != nil {
		return store.IssueMutationResult{}, err
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

func lockIssueStatusRunFence(ctx context.Context, tx pgx.Tx, input store.IssueStatusMutation) error {
	if input.RunID == nil {
		return nil
	}
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, input.ProjectID, *input.RunID))
	if err != nil {
		return notFound(err)
	}
	if run.Status != "RUNNING" || run.IssueID != input.IssueID || run.WorkspaceID != *input.WorkspaceID ||
		run.AgentID == nil || *run.AgentID != *input.AgentID {
		return store.ErrConflict
	}
	return nil
}
