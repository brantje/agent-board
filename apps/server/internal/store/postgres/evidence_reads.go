package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Store) ListRawOutputChunks(ctx context.Context, projectID, runID string) ([]store.RawOutputChunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, stream, sequence, storage_ref, size_bytes, digest, created_at
		FROM raw_output_chunks
		WHERE project_id = $1 AND run_id = $2
		ORDER BY stream, sequence
	`, projectID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]store.RawOutputChunk, 0)
	for rows.Next() {
		value, err := scanRawOutputChunk(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
