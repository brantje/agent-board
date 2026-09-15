package postgres

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) UpdateIssuePatchMutation(ctx context.Context, patch store.IssuePatch, actor json.RawMessage) (store.IssueMutationResult, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if patch.Status != nil {
		if err := lockIssueBoardOrder(ctx, tx, patch.ProjectID); err != nil {
			return store.IssueMutationResult{}, err
		}
	}

	previous, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, patch.ProjectID, patch.ID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	updated := previous
	if patch.Title != nil {
		updated.Title = *patch.Title
	}
	if patch.Description != nil {
		updated.Description = *patch.Description
	}
	if patch.Status != nil {
		updated.Status = *patch.Status
	}
	if patch.Priority != nil {
		updated.Priority = *patch.Priority
	}
	if updated.Title == previous.Title && updated.Description == previous.Description && updated.Status == previous.Status && updated.Priority == previous.Priority {
		current, err := getIssue(ctx, tx, patch.ProjectID, patch.ID)
		if err != nil {
			return store.IssueMutationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return store.IssueMutationResult{}, err
		}
		return store.IssueMutationResult{Issue: current}, nil
	}
	if updated.Status != previous.Status {
		if err := moveIssueToStatusTopTx(ctx, tx, patch.ProjectID, patch.ID, previous.Status, updated.Status); err != nil {
			return store.IssueMutationResult{}, err
		}
	}

	result, err := s.applyIssueMutationTx(ctx, tx, issueMutationTxInput{
		Issue:          updated,
		Previous:       previous,
		RepositoryPath: repositoryPath,
		DefaultBranch:  defaultBranch,
		Actor:          actor,
	})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.IssueMutationResult{}, err
	}
	return result, nil
}
