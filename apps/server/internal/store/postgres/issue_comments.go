package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListIssueComments(ctx context.Context, projectID, issueID string) ([]store.IssueComment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id::text, c.issue_id::text, c.parent_comment_id::text, c.author_type, c.author_id::text,
		       COALESCE(
		           CASE
		               WHEN c.author_type = 'HUMAN' THEN (SELECT u.display_name FROM users AS u WHERE u.id = c.author_id)
		               WHEN c.author_type = 'AGENT' THEN (SELECT a.name FROM agents AS a WHERE a.id = c.author_id AND (a.project_id IS NULL OR a.project_id = i.project_id))
		           END,
		           ''
		       ),
		       COALESCE(c.body, ''), c.deleted_at, c.resolved_at, c.resolved_by_user_id::text,
		       COALESCE((SELECT u.display_name FROM users AS u WHERE u.id = c.resolved_by_user_id), ''),
		       c.created_at, c.updated_at
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.loadIssueCommentReactions(ctx, projectID, issueID, values); err != nil {
		return nil, err
	}
	return values, nil
}

func (s *Store) GetIssueComment(ctx context.Context, projectID, issueID, commentID string) (store.IssueComment, error) {
	value, err := scanIssueComment(s.pool.QueryRow(ctx, `
		SELECT c.id::text, c.issue_id::text, c.parent_comment_id::text, c.author_type, c.author_id::text,
		       COALESCE(
		           CASE
		               WHEN c.author_type = 'HUMAN' THEN (SELECT u.display_name FROM users AS u WHERE u.id = c.author_id)
		               WHEN c.author_type = 'AGENT' THEN (SELECT a.name FROM agents AS a WHERE a.id = c.author_id AND (a.project_id IS NULL OR a.project_id = i.project_id))
		           END,
		           ''
		       ),
		       COALESCE(c.body, ''), c.deleted_at, c.resolved_at, c.resolved_by_user_id::text,
		       COALESCE((SELECT u.display_name FROM users AS u WHERE u.id = c.resolved_by_user_id), ''),
		       c.created_at, c.updated_at
		FROM issue_comments AS c
		JOIN issues AS i ON i.id = c.issue_id
		WHERE i.project_id = $1 AND c.issue_id = $2 AND c.id = $3
	`, projectID, issueID, commentID))
	if err != nil {
		return store.IssueComment{}, err
	}
	values := []store.IssueComment{value}
	if err := s.loadIssueCommentReactions(ctx, projectID, issueID, values); err != nil {
		return store.IssueComment{}, err
	}
	return values[0], nil
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
			WHERE issue_id = $1 AND id = $2 AND deleted_at IS NULL
			FOR KEY SHARE
		`, issueID, *input.ParentCommentID).Scan(&parentID); err != nil {
			return store.IssueCommentMutationResult{}, notFound(err)
		}
	}

	comment, err := scanIssueComment(tx.QueryRow(ctx, `
		INSERT INTO issue_comments (issue_id, parent_comment_id, author_type, author_id, body)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, issue_id::text, parent_comment_id::text, author_type, author_id::text,
		          $6::text, COALESCE(body, ''), deleted_at, resolved_at, resolved_by_user_id::text,
		          ''::text, created_at, updated_at
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

func (s *Store) UpdateIssueComment(ctx context.Context, projectID, issueID, commentID, actorID, body string) (store.IssueCommentMutationResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || strings.TrimSpace(commentID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(body) == "" {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentBody string
	var deletedAt *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(c.body, ''), c.deleted_at
		FROM issue_comments AS c
		JOIN issues AS i ON i.id = c.issue_id
		WHERE i.project_id = $1 AND c.issue_id = $2 AND c.id = $3
		FOR UPDATE OF c
	`, projectID, issueID, commentID).Scan(&currentBody, &deletedAt); err != nil {
		return store.IssueCommentMutationResult{}, notFound(err)
	}
	if deletedAt != nil {
		return store.IssueCommentMutationResult{}, store.ErrConflict
	}
	if currentBody == body {
		if err := tx.Commit(ctx); err != nil {
			return store.IssueCommentMutationResult{}, err
		}
		comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
		return store.IssueCommentMutationResult{Comment: comment}, err
	}

	var changedAt time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE issue_comments
		SET body = $4, updated_at = now()
		WHERE issue_id = $2 AND id = $3
		RETURNING updated_at
	`, projectID, issueID, commentID, body).Scan(&changedAt); err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	event, err := appendIssueCommentChangedEventTx(ctx, tx, projectID, issueID, commentID, actorID, "EDITED", nil, changedAt)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	return store.IssueCommentMutationResult{Comment: comment, Events: []store.Event{event}}, nil
}

func (s *Store) DeleteIssueComment(ctx context.Context, projectID, issueID, commentID, actorID string) (store.IssueCommentDeleteResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || strings.TrimSpace(commentID) == "" || strings.TrimSpace(actorID) == "" {
		return store.IssueCommentDeleteResult{}, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueCommentDeleteResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var deletedAt *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT c.deleted_at
		FROM issue_comments AS c
		JOIN issues AS i ON i.id = c.issue_id
		WHERE i.project_id = $1 AND c.issue_id = $2 AND c.id = $3
		FOR UPDATE OF c
	`, projectID, issueID, commentID).Scan(&deletedAt); err != nil {
		return store.IssueCommentDeleteResult{}, notFound(err)
	}
	if deletedAt != nil {
		if err := tx.Commit(ctx); err != nil {
			return store.IssueCommentDeleteResult{}, err
		}
		return store.IssueCommentDeleteResult{}, nil
	}

	var hasReplies bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM issue_comments
			WHERE issue_id = $1 AND parent_comment_id = $2
		)
	`, issueID, commentID).Scan(&hasReplies); err != nil {
		return store.IssueCommentDeleteResult{}, err
	}

	var changedAt time.Time
	if hasReplies {
		if _, err := tx.Exec(ctx, `DELETE FROM issue_comment_reactions WHERE issue_id = $1 AND comment_id = $2`, issueID, commentID); err != nil {
			return store.IssueCommentDeleteResult{}, err
		}
		if err := tx.QueryRow(ctx, `
			UPDATE issue_comments
			SET body = NULL, deleted_at = now(), resolved_at = NULL, resolved_by_user_id = NULL
			WHERE issue_id = $1 AND id = $2
			RETURNING deleted_at
		`, issueID, commentID).Scan(&changedAt); err != nil {
			return store.IssueCommentDeleteResult{}, err
		}
	} else {
		if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&changedAt); err != nil {
			return store.IssueCommentDeleteResult{}, err
		}
		command, err := tx.Exec(ctx, `DELETE FROM issue_comments WHERE issue_id = $1 AND id = $2`, issueID, commentID)
		if err != nil {
			return store.IssueCommentDeleteResult{}, err
		}
		if command.RowsAffected() != 1 {
			return store.IssueCommentDeleteResult{}, store.ErrNotFound
		}
	}

	event, err := appendIssueCommentChangedEventTx(ctx, tx, projectID, issueID, commentID, actorID, "DELETED", nil, changedAt)
	if err != nil {
		return store.IssueCommentDeleteResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.IssueCommentDeleteResult{}, err
	}
	return store.IssueCommentDeleteResult{Events: []store.Event{event}}, nil
}

func (s *Store) ResolveIssueComment(ctx context.Context, projectID, issueID, commentID, actorID string) (store.IssueCommentMutationResult, error) {
	return s.setIssueCommentResolution(ctx, projectID, issueID, commentID, actorID, true)
}

func (s *Store) ReopenIssueComment(ctx context.Context, projectID, issueID, commentID, actorID string) (store.IssueCommentMutationResult, error) {
	return s.setIssueCommentResolution(ctx, projectID, issueID, commentID, actorID, false)
}

func (s *Store) setIssueCommentResolution(ctx context.Context, projectID, issueID, commentID, actorID string, resolved bool) (store.IssueCommentMutationResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || strings.TrimSpace(commentID) == "" || strings.TrimSpace(actorID) == "" {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var parentID *string
	var deletedAt, resolvedAt *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT c.parent_comment_id::text, c.deleted_at, c.resolved_at
		FROM issue_comments AS c
		JOIN issues AS i ON i.id = c.issue_id
		WHERE i.project_id = $1 AND c.issue_id = $2 AND c.id = $3
		FOR UPDATE OF c
	`, projectID, issueID, commentID).Scan(&parentID, &deletedAt, &resolvedAt); err != nil {
		return store.IssueCommentMutationResult{}, notFound(err)
	}
	if parentID != nil {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	if deletedAt != nil {
		return store.IssueCommentMutationResult{}, store.ErrConflict
	}
	if resolved == (resolvedAt != nil) {
		if err := tx.Commit(ctx); err != nil {
			return store.IssueCommentMutationResult{}, err
		}
		comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
		return store.IssueCommentMutationResult{Comment: comment}, err
	}

	var changedAt time.Time
	if resolved {
		if err := tx.QueryRow(ctx, `
			UPDATE issue_comments
			SET resolved_at = now(), resolved_by_user_id = $3
			WHERE issue_id = $1 AND id = $2
			RETURNING resolved_at
		`, issueID, commentID, actorID).Scan(&changedAt); err != nil {
			return store.IssueCommentMutationResult{}, err
		}
	} else {
		if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&changedAt); err != nil {
			return store.IssueCommentMutationResult{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE issue_comments
			SET resolved_at = NULL, resolved_by_user_id = NULL
			WHERE issue_id = $1 AND id = $2
		`, issueID, commentID); err != nil {
			return store.IssueCommentMutationResult{}, err
		}
	}

	change := "REOPENED"
	if resolved {
		change = "RESOLVED"
	}
	event, err := appendIssueCommentChangedEventTx(ctx, tx, projectID, issueID, commentID, actorID, change, nil, changedAt)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	return store.IssueCommentMutationResult{Comment: comment, Events: []store.Event{event}}, nil
}

func (s *Store) AddIssueCommentReaction(ctx context.Context, projectID, issueID, commentID, actorID, reaction string) ([]store.Event, error) {
	return s.mutateIssueCommentReaction(ctx, projectID, issueID, commentID, actorID, reaction, true)
}

func (s *Store) RemoveIssueCommentReaction(ctx context.Context, projectID, issueID, commentID, actorID, reaction string) ([]store.Event, error) {
	return s.mutateIssueCommentReaction(ctx, projectID, issueID, commentID, actorID, reaction, false)
}

func (s *Store) mutateIssueCommentReaction(ctx context.Context, projectID, issueID, commentID, actorID, reaction string, add bool) ([]store.Event, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || strings.TrimSpace(commentID) == "" || strings.TrimSpace(actorID) == "" || !store.ValidIssueCommentReaction(reaction) {
		return nil, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var deletedAt *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT c.deleted_at
		FROM issue_comments AS c
		JOIN issues AS i ON i.id = c.issue_id
		WHERE i.project_id = $1 AND c.issue_id = $2 AND c.id = $3
		FOR KEY SHARE OF c
	`, projectID, issueID, commentID).Scan(&deletedAt); err != nil {
		return nil, notFound(err)
	}
	if deletedAt != nil {
		return nil, store.ErrConflict
	}

	var affected int64
	if add {
		tag, err := tx.Exec(ctx, `
			INSERT INTO issue_comment_reactions (issue_id, comment_id, actor_id, reaction)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING
		`, issueID, commentID, actorID, reaction)
		if err != nil {
			return nil, err
		}
		affected = tag.RowsAffected()
	} else {
		tag, err := tx.Exec(ctx, `
			DELETE FROM issue_comment_reactions
			WHERE issue_id = $1 AND comment_id = $2 AND actor_id = $3 AND reaction = $4
		`, issueID, commentID, actorID, reaction)
		if err != nil {
			return nil, err
		}
		affected = tag.RowsAffected()
	}
	if affected == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}

	var changedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&changedAt); err != nil {
		return nil, err
	}
	change := "REACTION_REMOVED"
	if add {
		change = "REACTION_ADDED"
	}
	event, err := appendIssueCommentChangedEventTx(ctx, tx, projectID, issueID, commentID, actorID, change, &reaction, changedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return []store.Event{event}, nil
}

func (s *Store) loadIssueCommentReactions(ctx context.Context, projectID, issueID string, values []store.IssueComment) error {
	if len(values) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.comment_id::text, r.reaction, r.actor_id::text
		FROM issue_comment_reactions AS r
		JOIN issues AS i ON i.id = r.issue_id
		WHERE i.project_id = $1 AND r.issue_id = $2
		ORDER BY r.comment_id, r.reaction, r.actor_id
	`, projectID, issueID)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[string]int, len(values))
	for index := range values {
		byID[values[index].ID] = index
	}
	for rows.Next() {
		var commentID, reaction, actorID string
		if err := rows.Scan(&commentID, &reaction, &actorID); err != nil {
			return err
		}
		index, ok := byID[commentID]
		if !ok {
			continue
		}
		found := false
		for summaryIndex := range values[index].Reactions {
			if values[index].Reactions[summaryIndex].Reaction == reaction {
				values[index].Reactions[summaryIndex].Count++
				values[index].Reactions[summaryIndex].ActorIDs = append(values[index].Reactions[summaryIndex].ActorIDs, actorID)
				found = true
				break
			}
		}
		if !found {
			values[index].Reactions = append(values[index].Reactions, store.IssueCommentReactionSummary{
				Reaction: reaction,
				Count:    1,
				ActorIDs: []string{actorID},
			})
		}
	}
	return rows.Err()
}

func appendIssueCommentChangedEventTx(ctx context.Context, tx pgx.Tx, projectID, issueID, commentID, actorID, change string, reaction *string, occurredAt time.Time) (store.Event, error) {
	actor, err := json.Marshal(map[string]string{"type": store.ActorTypeHuman, "id": actorID})
	if err != nil {
		return store.Event{}, err
	}
	payload := map[string]any{"commentId": commentID, "change": change}
	if reaction != nil {
		payload["reaction"] = *reaction
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return store.Event{}, err
	}
	return appendEventTx(ctx, tx, store.Event{
		Type:       "issue.comment_changed",
		OccurredAt: occurredAt,
		ProjectID:  projectID,
		IssueID:    &issueID,
		Actor:      actor,
		Payload:    encodedPayload,
	})
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
		&value.DeletedAt,
		&value.ResolvedAt,
		&value.ResolvedByUserID,
		&value.ResolvedByName,
		&value.CreatedAt,
		&value.UpdatedAt,
	); err != nil {
		return store.IssueComment{}, notFound(err)
	}
	return value, nil
}
