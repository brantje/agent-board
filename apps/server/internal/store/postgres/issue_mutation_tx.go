package postgres

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

type issueMutationTxInput struct {
	Issue          store.Issue
	Previous       store.Issue
	RepositoryPath string
	DefaultBranch  string
	Actor          json.RawMessage
	RunID          *string
	AgentID        *string
	WorkspaceID    *string
}

func applyIssueMutationTx(ctx context.Context, tx pgx.Tx, input issueMutationTxInput) (store.IssueMutationResult, error) {
	updated, err := scanIssueJoined(tx.QueryRow(ctx, `
		UPDATE issues AS i SET title=$3, description=$4, status=$5, priority=$6, updated_at=now()
		FROM projects AS p
		WHERE i.project_id=$1 AND i.id=$2 AND p.id=i.project_id
		RETURNING `+issueSelectColumns+`
	`, input.Issue.ProjectID, input.Issue.ID, input.Issue.Title, input.Issue.Description, input.Issue.Status, input.Issue.Priority))
	if err != nil {
		return store.IssueMutationResult{}, err
	}

	_, runEvent, err := enqueueIssueMutation(ctx, tx, updated, input.Previous.Status, false, input.RepositoryPath, input.DefaultBranch)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	updated.PreviousStatus = input.Previous.Status
	issueEvent, err := store.NewIssueUpdatedEventWithActor(updated, input.Previous.Status, input.Actor)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	issueEvent.RunID = input.RunID
	issueEvent.AgentID = input.AgentID
	issueEvent.WorkspaceID = input.WorkspaceID
	issueEvent, err = appendEventTx(ctx, tx, issueEvent)
	if err != nil {
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
