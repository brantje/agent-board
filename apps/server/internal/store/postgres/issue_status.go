package postgres

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) SetIssueStatus(ctx context.Context, projectID, issueID, status string, actor json.RawMessage) (store.IssueMutationResult, error) {
	if !store.ValidIssueStatus(status) {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	previous, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, projectID, issueID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if previous.Status == status {
		current, err := getIssue(ctx, tx, projectID, issueID)
		if err != nil {
			return store.IssueMutationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return store.IssueMutationResult{}, err
		}
		return store.IssueMutationResult{Issue: current}, nil
	}

	updated, err := scanIssueJoined(tx.QueryRow(ctx, `
		UPDATE issues AS i SET status=$3, updated_at=now()
		FROM projects AS p
		WHERE i.project_id=$1 AND i.id=$2 AND p.id=i.project_id
		RETURNING `+issueSelectColumns+`
	`, projectID, issueID, status))
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	_, runEvent, err := enqueueIssueMutation(ctx, tx, updated, previous.Status, false, repositoryPath, defaultBranch)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	updated.PreviousStatus = previous.Status
	issueEvent, err := store.NewIssueUpdatedEventWithActor(updated, previous.Status, actor)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	issueEvent, err = appendEventTx(ctx, tx, issueEvent)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.IssueMutationResult{}, err
	}

	events := make([]store.Event, 0, 2)
	if runEvent.ID != "" {
		events = append(events, runEvent)
	}
	events = append(events, issueEvent)
	updated.LastEvent = &issueEvent
	return store.IssueMutationResult{Issue: updated, Events: events}, nil
}
