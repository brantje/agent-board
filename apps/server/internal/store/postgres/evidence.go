package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) PutRunProvenance(ctx context.Context, projectID, runID string, snapshot json.RawMessage) error {
	command, err := s.pool.Exec(ctx, `
		INSERT INTO run_provenance (project_id, run_id, snapshot)
		SELECT $1, id, $3
		FROM runs
		WHERE project_id = $1 AND id = $2
	`, projectID, runID, objectJSON(snapshot))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetRunProvenance(ctx context.Context, projectID, runID string) (json.RawMessage, error) {
	var snapshot json.RawMessage
	if err := s.pool.QueryRow(ctx, `
		SELECT snapshot
		FROM run_provenance
		WHERE project_id = $1 AND run_id = $2
	`, projectID, runID).Scan(&snapshot); err != nil {
		return nil, notFound(err)
	}
	return snapshot, nil
}

func (s *Store) AppendEvent(ctx context.Context, input store.Event) (store.Event, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.Event{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

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
	if err := tx.Commit(ctx); err != nil {
		return store.Event{}, err
	}
	return value, nil
}

func validateEventReferences(ctx context.Context, tx pgx.Tx, input store.Event) error {
	var valid bool
	if err := tx.QueryRow(ctx, `
		SELECT
			($2::uuid IS NULL OR EXISTS (
				SELECT 1 FROM agents AS agent
				WHERE agent.id = $2 AND (agent.project_id IS NULL OR agent.project_id = $1)
			))
			AND
			($3::uuid IS NULL OR EXISTS (
				SELECT 1 FROM events AS parent
				WHERE parent.id = $3 AND parent.project_id = $1
			))
	`, input.ProjectID, input.AgentID, input.ParentEventID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListRunEvents(ctx context.Context, projectID, runID string, afterSequence int64, limit int) ([]store.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, schema_version, type, occurred_at, project_id::text, issue_id::text,
		       run_id::text, agent_id::text, workspace_id::text, runtime_instance_id::text,
		       correlation_id::text, parent_event_id::text, sequence, actor, payload, created_at
		FROM events
		WHERE project_id = $1 AND run_id = $2 AND sequence > $3
		ORDER BY sequence
		LIMIT $4
	`, projectID, runID, afterSequence, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]store.Event, 0)
	for rows.Next() {
		value, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) CreateRawOutputChunk(ctx context.Context, input store.RawOutputChunk) (store.RawOutputChunk, error) {
	return scanRawOutputChunk(s.pool.QueryRow(ctx, `
		INSERT INTO raw_output_chunks (project_id, issue_id, run_id, stream, sequence, storage_ref, size_bytes, digest)
		SELECT $1, run.issue_id, run.id, $4, $5, $6, $7, $8
		FROM runs AS run
		WHERE run.project_id = $1 AND run.id = $3 AND run.issue_id = $2
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, stream, sequence, storage_ref, size_bytes, digest, created_at
	`, input.ProjectID, input.IssueID, input.RunID, input.Stream, input.Sequence, input.StorageRef, input.SizeBytes, input.Digest))
}

func (s *Store) CreateArtifact(ctx context.Context, input store.Artifact) (store.Artifact, error) {
	return scanArtifact(s.pool.QueryRow(ctx, `
		INSERT INTO artifacts (project_id, issue_id, run_id, name, kind, media_type, size_bytes, digest, storage_ref, safe_metadata)
		SELECT $1, run.issue_id, run.id, $4, $5, $6, $7, $8, $9, $10
		FROM runs AS run
		WHERE run.project_id = $1 AND run.id = $3 AND run.issue_id = $2
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, name, kind, media_type, size_bytes, digest, storage_ref, safe_metadata, created_at, deleted_at
	`, input.ProjectID, input.IssueID, input.RunID, input.Name, input.Kind, input.MediaType, input.SizeBytes, input.Digest, input.StorageRef, objectJSON(input.SafeMetadata)))
}

func (s *Store) ListArtifacts(ctx context.Context, projectID, runID string) ([]store.Artifact, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, name, kind, media_type, size_bytes, digest, storage_ref, safe_metadata, created_at, deleted_at
		FROM artifacts
		WHERE project_id = $1 AND run_id = $2 AND deleted_at IS NULL
		ORDER BY created_at, id
	`, projectID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]store.Artifact, 0)
	for rows.Next() {
		value, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func scanEvent(row pgx.Row) (store.Event, error) {
	var value store.Event
	if err := row.Scan(&value.ID, &value.SchemaVersion, &value.Type, &value.OccurredAt, &value.ProjectID,
		&value.IssueID, &value.RunID, &value.AgentID, &value.WorkspaceID, &value.RuntimeInstanceID,
		&value.CorrelationID, &value.ParentEventID, &value.Sequence, &value.Actor, &value.Payload, &value.CreatedAt); err != nil {
		return store.Event{}, notFound(err)
	}
	return value, nil
}

func scanRawOutputChunk(row pgx.Row) (store.RawOutputChunk, error) {
	var value store.RawOutputChunk
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.RunID, &value.Stream, &value.Sequence,
		&value.StorageRef, &value.SizeBytes, &value.Digest, &value.CreatedAt); err != nil {
		return store.RawOutputChunk{}, notFound(err)
	}
	return value, nil
}

func scanArtifact(row pgx.Row) (store.Artifact, error) {
	var value store.Artifact
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.RunID, &value.Name, &value.Kind,
		&value.MediaType, &value.SizeBytes, &value.Digest, &value.StorageRef, &value.SafeMetadata, &value.CreatedAt, &value.DeletedAt); err != nil {
		return store.Artifact{}, notFound(err)
	}
	return value, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
