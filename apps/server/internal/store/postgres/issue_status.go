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
	if err := lockIssueBoardProject(ctx, tx, input.ProjectID); err != nil {
		return store.IssueMutationResult{}, err
	}

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
		return store.Run{}, notFound(err)
	}
	if run.IssueID != input.IssueID || run.WorkspaceID != *input.WorkspaceID ||
		run.AgentID == nil || *run.AgentID != *input.AgentID {
		return store.Run{}, store.ErrConflict
	}
	if run.Status == "RUNNING" {
		return run, nil
	}
	if run.Status == "WAITING_FOR_INPUT" {
		ready, err := runHasAnsweredInteractiveInput(ctx, tx, run.ProjectID, run.ID)
		if err != nil {
			return store.Run{}, err
		}
		if ready {
			return run, nil
		}
	}
	return store.Run{}, store.ErrConflict
}

func runHasAnsweredInteractiveInput(ctx context.Context, tx pgx.Tx, projectID, runID string) (bool, error) {
	var ready bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM questions AS q
			JOIN engine_question_bindings AS binding
			  ON binding.project_id=q.project_id
			 AND binding.run_id=q.run_id
			 AND binding.question_id=q.id
			WHERE q.project_id=$1 AND q.run_id=$2 AND q.blocking AND q.status='ANSWERED'
			  AND binding.state='ANSWERED'
		)
		AND NOT EXISTS (
			SELECT 1
			FROM engine_question_bindings AS pending
			WHERE pending.project_id=$1 AND pending.run_id=$2 AND pending.state='OPEN'
		)
	`, projectID, runID).Scan(&ready)
	return ready, err
}

func validateIssueStatusRecoveryFence(ctx context.Context, tx pgx.Tx, input store.IssueStatusMutation, run store.Run) error {
	if input.RunID == nil || input.AgentID == nil || input.WorkspaceID == nil || run.StartedAt == nil {
		return store.ErrConflict
	}

	// OpenCode durable history does not share a trusted clock or sequence with
	// Agent Board's Event stream, especially on external runners. Treat canonical
	// status Events proven to come from this exact Run capability as earlier
	// same-Run progress, but conservatively fence any other explicit Issue mutation.
	var superseded bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM events
			WHERE project_id=$1 AND issue_id=$2
			  AND type IN ('issue.updated', 'issue.status_changed')
			  AND created_at >= $3
			  AND NOT (
				type='issue.status_changed'
				AND run_id IS NOT NULL AND run_id=$4
				AND agent_id IS NOT NULL AND agent_id=$5
				AND workspace_id IS NOT NULL AND workspace_id=$6
				AND COALESCE(actor->>'type','')=$7
				AND COALESCE(actor->>'id','')=$5::text
			  )
		)
	`, input.ProjectID, input.IssueID, *run.StartedAt, *input.RunID, *input.AgentID, *input.WorkspaceID, store.ActorTypeAgent).Scan(&superseded); err != nil {
		return err
	}
	if superseded {
		return store.ErrIssueStatusRecoverySuperseded
	}
	return nil
}
