package postgres

import (
	"context"
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
	if input.Recovery != (input.RecoveryAt != nil) || (input.RecoveryAt != nil && input.RecoveryAt.IsZero()) {
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
	if input.RecoveryAt == nil || input.RecoveryAt.IsZero() {
		return store.ErrInvalidArgument
	}

	checkpoint := input.RecoveryAt.UTC()
	startedAt := run.StartedAt.UTC()
	var databaseNow time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
		return err
	}
	// OpenCode records tool completion on the execution host. If that clock is
	// obviously outside this Run's server-owned interval, fall back to the Run
	// start as a conservative checkpoint rather than risk overwriting a known
	// newer Issue mutation.
	if checkpoint.Before(startedAt) || checkpoint.After(databaseNow) {
		checkpoint = startedAt
	}

	var superseded bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM events
			WHERE project_id=$1 AND issue_id=$2
			  AND type IN ('issue.updated', 'issue.status_changed')
			  AND created_at >= $3
		)
	`, input.ProjectID, input.IssueID, checkpoint).Scan(&superseded); err != nil {
		return err
	}
	if superseded {
		return store.ErrIssueStatusRecoverySuperseded
	}
	return nil
}
