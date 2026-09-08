package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// appendEventTx appends a canonical Event inside an existing transaction. It is
// used when the state change and its audit Event must commit atomically.
func appendEventTx(ctx context.Context, tx pgx.Tx, input store.Event) (store.Event, error) {
	if err := validateEventReferences(ctx, tx, input); err != nil {
		return store.Event{}, err
	}

	if input.RunID != nil {
		var sequence int64
		if err := tx.QueryRow(ctx, `
			UPDATE runs
			SET event_sequence = event_sequence + 1
			WHERE project_id = $1 AND id = $2
			RETURNING event_sequence
		`, input.ProjectID, *input.RunID).Scan(&sequence); err != nil {
			return store.Event{}, notFound(err)
		}
		input.Sequence = &sequence
	} else {
		input.Sequence = nil
	}

	schemaVersion := input.SchemaVersion
	if schemaVersion == 0 {
		schemaVersion = 1
	}
	value, err := scanEvent(tx.QueryRow(ctx, `
		INSERT INTO events (
			schema_version, type, occurred_at, project_id, issue_id, run_id, agent_id,
			workspace_id, runtime_instance_id, correlation_id, parent_event_id, sequence, actor, payload
		)
		VALUES ($1, $2, COALESCE($3, now()), $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id::text, schema_version, type, occurred_at, project_id::text, issue_id::text,
		          run_id::text, agent_id::text, workspace_id::text, runtime_instance_id::text,
		          correlation_id::text, parent_event_id::text, sequence, actor, payload, created_at
	`, schemaVersion, input.Type, nullableTime(input.OccurredAt), input.ProjectID, input.IssueID, input.RunID,
		input.AgentID, input.WorkspaceID, input.RuntimeInstanceID, input.CorrelationID, input.ParentEventID,
		input.Sequence, objectJSON(input.Actor), objectJSON(input.Payload)))
	if err != nil {
		return store.Event{}, err
	}
	return value, nil
}
