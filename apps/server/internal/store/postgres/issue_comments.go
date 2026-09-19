package postgres

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListIssueComments(ctx context.Context, projectID, issueID string) ([]store.IssueComment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id::text, c.issue_id::text, c.parent_comment_id::text, c.author_type, c.author_id::text,
		       CASE
		           WHEN c.author_type = 'HUMAN' THEN (SELECT u.display_name FROM users AS u WHERE u.id = c.author_id)
		           WHEN c.author_type = 'AGENT' THEN (SELECT a.name FROM agents AS a WHERE a.id = c.author_id AND (a.project_id IS NULL OR a.project_id = i.project_id))
		           ELSE NULL
		       END,
		       c.body, c.created_at, c.updated_at
		FROM issue_comments AS c
		JOIN issues AS i ON i.id = c.issue_id
		WHERE i.project_id = $1 AND c.issue_id = $2
		ORDER BY c.created_at, c.id
	`, projectID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]store.IssueComment, 0)
	for rows.Next() {
		value, err := scanIssueComment(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) CreateIssueComment(ctx context.Context, projectID string, input store.IssueComment) (store.IssueCommentMutationResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(input.IssueID) == "" || strings.TrimSpace(input.AuthorID) == "" || strings.TrimSpace(input.Body) == "" || !store.ValidActorType(input.AuthorType) {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	if input.ParentCommentID != nil && strings.TrimSpace(*input.ParentCommentID) == "" {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var issueID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM issues
		WHERE project_id = $1 AND id = $2
		FOR SHARE
	`, projectID, input.IssueID).Scan(&issueID); err != nil {
		return store.IssueCommentMutationResult{}, notFound(err)
	}

	authorName, err := issueCommentAuthorName(ctx, tx, projectID, input.AuthorType, input.AuthorID)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	if input.ParentCommentID != nil {
		var parentID string
		if err := tx.QueryRow(ctx, `
			SELECT id::text
			FROM issue_comments
			WHERE issue_id = $1 AND id = $2
		`, issueID, *input.ParentCommentID).Scan(&parentID); err != nil {
			return store.IssueCommentMutationResult{}, notFound(err)
		}
	}

	comment, err := scanIssueComment(tx.QueryRow(ctx, `
		INSERT INTO issue_comments (issue_id, parent_comment_id, author_type, author_id, body)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, issue_id::text, parent_comment_id::text, author_type, author_id::text,
		          $6::text, body, created_at, updated_at
	`, issueID, input.ParentCommentID, input.AuthorType, input.AuthorID, input.Body, authorName))
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}

	actor, err := json.Marshal(map[string]string{"type": input.AuthorType, "id": input.AuthorID})
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	payload := map[string]any{"commentId": comment.ID}
	if comment.ParentCommentID != nil {
		payload["parentCommentId"] = *comment.ParentCommentID
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	event, err := appendEventTx(ctx, tx, store.Event{
		Type:       "issue.comment_created",
		OccurredAt: comment.CreatedAt,
		ProjectID:  projectID,
		IssueID:    &issueID,
		Actor:      actor,
		Payload:    encodedPayload,
	})
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	return store.IssueCommentMutationResult{Comment: comment, Events: []store.Event{event}}, nil
}

func issueCommentAuthorName(ctx context.Context, tx pgx.Tx, projectID, authorType, authorID string) (string, error) {
	var name string
	var err error
	switch authorType {
	case store.ActorTypeHuman:
		err = tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, authorID).Scan(&name)
	case store.ActorTypeAgent:
		err = tx.QueryRow(ctx, `SELECT name FROM agents WHERE id = $1 AND (project_id IS NULL OR project_id = $2)`, authorID, projectID).Scan(&name)
	default:
		return "", store.ErrInvalidArgument
	}
	if err != nil {
		return "", notFound(err)
	}
	return name, nil
}

func scanIssueComment(row pgx.Row) (store.IssueComment, error) {
	var value store.IssueComment
	if err := row.Scan(
		&value.ID,
		&value.IssueID,
		&value.ParentCommentID,
		&value.AuthorType,
		&value.AuthorID,
		&value.AuthorName,
		&value.Body,
		&value.CreatedAt,
		&value.UpdatedAt,
	); err != nil {
		return store.IssueComment{}, notFound(err)
	}
	return value, nil
}
