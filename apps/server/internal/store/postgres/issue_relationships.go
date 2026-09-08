package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func scanIssueRelationship(row pgx.Row) (store.IssueRelationship, error) {
	var value store.IssueRelationship
	if err := row.Scan(&value.ID, &value.ProjectID, &value.SourceIssueID, &value.TargetIssueID, &value.Type, &value.CreatedAt); err != nil {
		return store.IssueRelationship{}, notFound(err)
	}
	return value, nil
}

func (s *Store) ListIssueRelationships(ctx context.Context, projectID, sourceIssueID string) ([]store.IssueRelationship, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, project_id::text, source_issue_id::text, target_issue_id::text, type, created_at
		FROM issue_relationships
		WHERE project_id=$1 AND source_issue_id=$2
		ORDER BY created_at, id
	`, projectID, sourceIssueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]store.IssueRelationship, 0)
	for rows.Next() {
		value, err := scanIssueRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) CreateIssueRelationship(ctx context.Context, input store.IssueRelationship) (store.IssueRelationship, error) {
	return scanIssueRelationship(s.pool.QueryRow(ctx, `
		INSERT INTO issue_relationships (project_id, source_issue_id, target_issue_id, type)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, project_id::text, source_issue_id::text, target_issue_id::text, type, created_at
	`, input.ProjectID, input.SourceIssueID, input.TargetIssueID, input.Type))
}

func (s *Store) DeleteIssueRelationship(ctx context.Context, projectID, sourceIssueID, relationshipID string) error {
	result, err := s.pool.Exec(ctx, `
		DELETE FROM issue_relationships
		WHERE project_id=$1 AND source_issue_id=$2 AND id=$3
	`, projectID, sourceIssueID, relationshipID)
	if err != nil {
		return notFound(err)
	}
	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}
