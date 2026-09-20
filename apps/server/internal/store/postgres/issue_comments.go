package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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
		       c.source_run_id::text, c.source_action_key, COALESCE(c.body, ''), c.deleted_at, c.resolved_at, c.resolved_by_user_id::text,
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
	if err := s.loadIssueCommentMentions(ctx, projectID, issueID, values); err != nil {
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
		       c.source_run_id::text, c.source_action_key, COALESCE(c.body, ''), c.deleted_at, c.resolved_at, c.resolved_by_user_id::text,
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
	if err := s.loadIssueCommentMentions(ctx, projectID, issueID, values); err != nil {
		return store.IssueComment{}, err
	}
	return values[0], nil
}

func (s *Store) CreateIssueComment(ctx context.Context, projectID string, input store.IssueComment) (store.IssueCommentMutationResult, error) {
	return s.createIssueComment(ctx, projectID, input, nil)
}

func (s *Store) CreateIssueCommentWithMentions(ctx context.Context, projectID string, input store.IssueComment, mentionAgentIDs []string) (store.IssueCommentMutationResult, error) {
	return s.createIssueComment(ctx, projectID, input, mentionAgentIDs)
}

func (s *Store) createIssueComment(ctx context.Context, projectID string, input store.IssueComment, mentionAgentIDs []string) (store.IssueCommentMutationResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(input.IssueID) == "" || strings.TrimSpace(input.AuthorID) == "" || strings.TrimSpace(input.Body) == "" || !store.ValidActorType(input.AuthorType) {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	if input.ParentCommentID != nil && strings.TrimSpace(*input.ParentCommentID) == "" {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	mentions, err := normalizeIssueCommentMentionAgentIDs(mentionAgentIDs)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	if input.SourceActionKey != nil {
		value := strings.TrimSpace(*input.SourceActionKey)
		if value == "" {
			return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
		}
		input.SourceActionKey = &value
	}
	if len(mentions) != 0 && input.SourceActionKey == nil {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	switch input.AuthorType {
	case store.ActorTypeHuman:
		if input.SourceRunID != nil {
			return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
		}
	case store.ActorTypeAgent:
		if input.SourceRunID == nil || strings.TrimSpace(*input.SourceRunID) == "" || input.SourceActionKey == nil {
			return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
		}
	default:
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
	`, projectID, input.IssueID).Scan(&issueID); err != nil {
		return store.IssueCommentMutationResult{}, notFound(err)
	}

	authorName, err := issueCommentAuthorName(ctx, tx, projectID, input.AuthorType, input.AuthorID)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	if input.AuthorType == store.ActorTypeAgent {
		var runIssueID, runAgentID string
		if err := tx.QueryRow(ctx, `
			SELECT issue_id::text, COALESCE(agent_id::text, '')
			FROM runs
			WHERE project_id = $1 AND id = $2
			FOR SHARE
		`, projectID, *input.SourceRunID).Scan(&runIssueID, &runAgentID); err != nil {
			return store.IssueCommentMutationResult{}, notFound(err)
		}
		if runIssueID != issueID || runAgentID != input.AuthorID {
			return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
		}
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
		INSERT INTO issue_comments (issue_id, parent_comment_id, author_type, author_id, source_run_id, source_action_key, body)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT DO NOTHING
		RETURNING id::text, issue_id::text, parent_comment_id::text, author_type, author_id::text,
		          $8::text, source_run_id::text, source_action_key, COALESCE(body, ''), deleted_at, resolved_at, resolved_by_user_id::text,
		          ''::text, created_at, updated_at
	`, issueID, input.ParentCommentID, input.AuthorType, input.AuthorID, input.SourceRunID, input.SourceActionKey, input.Body, authorName))
	if errors.Is(err, store.ErrNotFound) && input.SourceActionKey != nil {
		existing, lookupErr := existingIssueCommentRequest(ctx, tx, issueID, authorName, input)
		if lookupErr != nil {
			return store.IssueCommentMutationResult{}, lookupErr
		}
		if !sameIssueCommentRequest(existing, input) {
			return store.IssueCommentMutationResult{}, store.ErrConflict
		}
		values := []store.IssueComment{existing}
		if err := loadIssueCommentMentionsWith(ctx, tx, projectID, issueID, values); err != nil {
			return store.IssueCommentMutationResult{}, err
		}
		if !sameIssueCommentMentionTargets(values[0].Mentions, mentions) {
			return store.IssueCommentMutationResult{}, store.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return store.IssueCommentMutationResult{}, err
		}
		return store.IssueCommentMutationResult{Comment: values[0]}, nil
	}
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}

	mentionValues, delegationEvents, err := s.createIssueCommentMentionsTx(ctx, tx, projectID, comment, mentions)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	comment.Mentions = mentionValues

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
	events := make([]store.Event, 0, 1+len(delegationEvents))
	events = append(events, event)
	events = append(events, delegationEvents...)
	return store.IssueCommentMutationResult{Comment: comment, Events: events}, nil
}

func existingIssueCommentRequest(ctx context.Context, tx pgx.Tx, issueID, authorName string, input store.IssueComment) (store.IssueComment, error) {
	if input.SourceActionKey == nil {
		return store.IssueComment{}, store.ErrNotFound
	}
	if input.AuthorType == store.ActorTypeAgent {
		return scanIssueComment(tx.QueryRow(ctx, `
			SELECT c.id::text, c.issue_id::text, c.parent_comment_id::text, c.author_type, c.author_id::text,
			       $3::text, c.source_run_id::text, c.source_action_key, COALESCE(c.body, ''),
			       c.deleted_at, c.resolved_at, c.resolved_by_user_id::text,
			       COALESCE(u.display_name, ''), c.created_at, c.updated_at
			FROM issue_comments AS c
			LEFT JOIN users AS u ON u.id = c.resolved_by_user_id
			WHERE c.source_run_id = $1 AND c.source_action_key = $2
		`, input.SourceRunID, input.SourceActionKey, authorName))
	}
	return scanIssueComment(tx.QueryRow(ctx, `
		SELECT c.id::text, c.issue_id::text, c.parent_comment_id::text, c.author_type, c.author_id::text,
		       $4::text, c.source_run_id::text, c.source_action_key, COALESCE(c.body, ''),
		       c.deleted_at, c.resolved_at, c.resolved_by_user_id::text,
		       COALESCE(u.display_name, ''), c.created_at, c.updated_at
		FROM issue_comments AS c
		LEFT JOIN users AS u ON u.id = c.resolved_by_user_id
		WHERE c.issue_id=$1 AND c.author_type='HUMAN' AND c.author_id=$2 AND c.source_action_key=$3
	`, issueID, input.AuthorID, input.SourceActionKey, authorName))
}

func (s *Store) createIssueCommentMentionsTx(ctx context.Context, tx pgx.Tx, projectID string, comment store.IssueComment, mentionAgentIDs []string) ([]store.IssueCommentMention, []store.Event, error) {
	if len(mentionAgentIDs) == 0 {
		return nil, nil, nil
	}
	result := make([]store.IssueCommentMention, 0, len(mentionAgentIDs))
	events := make([]store.Event, 0, len(mentionAgentIDs))
	for index, targetAgentID := range mentionAgentIDs {
		targetName := ""
		err := tx.QueryRow(ctx, `
			SELECT name
			FROM agents
			WHERE id=$2 AND (project_id IS NULL OR project_id=$1)
			FOR SHARE
		`, projectID, targetAgentID).Scan(&targetName)

		var delegationID *string
		var delegatedRunID *string
		var reasonCode *string
		outcome := store.IssueCommentMentionOutcomeBlocked
		if errors.Is(err, pgx.ErrNoRows) {
			reason := store.IssueCommentMentionReasonTargetUnavailable
			reasonCode = &reason
		} else if err != nil {
			return nil, nil, err
		} else if utf8.RuneCountInString(comment.Body) > store.MaxDelegationTaskCharacters {
			reason := store.IssueCommentMentionReasonDelegationBlocked
			reasonCode = &reason
		} else {
			requestKey := issueCommentMentionRequestKey(comment.ID, index, targetAgentID)
			var delegation store.RequestDelegationResult
			if comment.AuthorType == store.ActorTypeAgent {
				delegation, err = s.requestDelegationTx(ctx, tx, store.RequestDelegationCommand{
					ProjectID: projectID, ParentRunID: *comment.SourceRunID, TargetAgentID: targetAgentID,
					Task: comment.Body, RequestKey: requestKey,
				})
			} else {
				delegation, err = s.requestIssueDelegationTx(ctx, tx, store.RequestIssueDelegationCommand{
					ProjectID: projectID, IssueID: comment.IssueID, SourceCommentID: comment.ID, TargetAgentID: targetAgentID,
					Task: comment.Body, RequestKey: requestKey,
				})
			}
			if err == nil {
				outcome = store.IssueCommentMentionOutcomeQueued
				delegationID = &delegation.Delegation.ID
				delegatedRunID = &delegation.DelegatedRun.ID
				events = append(events, delegation.Events...)
			} else if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
				reason := store.IssueCommentMentionReasonDelegationBlocked
				reasonCode = &reason
			} else {
				return nil, nil, err
			}
		}

		var mention store.IssueCommentMention
		if err := tx.QueryRow(ctx, `
			INSERT INTO issue_comment_mentions (
				project_id, issue_id, comment_id, ordinal, target_agent_id, outcome, reason_code, delegation_id
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING id::text, target_agent_id::text, outcome, reason_code, delegation_id::text, created_at
		`, projectID, comment.IssueID, comment.ID, index, targetAgentID, outcome, reasonCode, delegationID).Scan(
			&mention.ID, &mention.TargetAgentID, &mention.Outcome, &mention.ReasonCode, &mention.DelegationID, &mention.CreatedAt,
		); err != nil {
			return nil, nil, err
		}
		mention.TargetAgentName = targetName
		mention.DelegatedRunID = delegatedRunID
		result = append(result, mention)
	}
	return result, events, nil
}

func issueCommentMentionRequestKey(commentID string, ordinal int, targetAgentID string) string {
	return fmt.Sprintf("comment:%s:mention:%d:%s", commentID, ordinal, targetAgentID)
}

func normalizeIssueCommentMentionAgentIDs(values []string) ([]string, error) {
	if len(values) > store.MaxIssueCommentMentions {
		return nil, store.ErrInvalidArgument
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if !validUUIDText(value) {
			return nil, store.ErrInvalidArgument
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, store.ErrInvalidArgument
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func validUUIDText(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
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
		SET body = $3, updated_at = now()
		WHERE issue_id = $1 AND id = $2
		RETURNING updated_at
	`, issueID, commentID, body).Scan(&changedAt); err != nil {
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

	var preserveComment bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM issue_comments
			WHERE issue_id = $1 AND parent_comment_id = $2
		) OR EXISTS (
			SELECT 1 FROM issue_comment_mentions
			WHERE issue_id = $1 AND comment_id = $2
		)
	`, issueID, commentID).Scan(&preserveComment); err != nil {
		return store.IssueCommentDeleteResult{}, err
	}

	var changedAt time.Time
	if preserveComment {
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

type issueCommentRowsQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s *Store) loadIssueCommentMentions(ctx context.Context, projectID, issueID string, values []store.IssueComment) error {
	return loadIssueCommentMentionsWith(ctx, s.pool, projectID, issueID, values)
}

func loadIssueCommentMentionsWith(ctx context.Context, q issueCommentRowsQuerier, projectID, issueID string, values []store.IssueComment) error {
	if len(values) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `
		SELECT m.comment_id::text, m.id::text, m.target_agent_id::text,
		       COALESCE(a.name, ''), m.outcome, m.reason_code, m.delegation_id::text,
		       d.delegated_run_id::text, m.created_at
		FROM issue_comment_mentions AS m
		LEFT JOIN agents AS a
		  ON a.id=m.target_agent_id
		 AND (a.project_id IS NULL OR a.project_id=m.project_id)
		LEFT JOIN delegations AS d
		  ON d.project_id=m.project_id
		 AND d.id=m.delegation_id
		WHERE m.project_id=$1 AND m.issue_id=$2
		ORDER BY m.comment_id, m.ordinal
	`, projectID, issueID)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[string]int, len(values))
	for index := range values {
		byID[values[index].ID] = index
		values[index].Mentions = nil
	}
	for rows.Next() {
		var commentID string
		var mention store.IssueCommentMention
		if err := rows.Scan(
			&commentID, &mention.ID, &mention.TargetAgentID, &mention.TargetAgentName,
			&mention.Outcome, &mention.ReasonCode, &mention.DelegationID, &mention.DelegatedRunID, &mention.CreatedAt,
		); err != nil {
			return err
		}
		index, ok := byID[commentID]
		if !ok {
			continue
		}
		values[index].Mentions = append(values[index].Mentions, mention)
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

func sameIssueCommentRequest(existing, requested store.IssueComment) bool {
	if existing.IssueID != requested.IssueID || existing.AuthorType != requested.AuthorType || existing.AuthorID != requested.AuthorID || existing.Body != requested.Body {
		return false
	}
	if (existing.ParentCommentID == nil) != (requested.ParentCommentID == nil) {
		return false
	}
	if existing.ParentCommentID != nil && *existing.ParentCommentID != *requested.ParentCommentID {
		return false
	}
	if existing.SourceActionKey == nil || requested.SourceActionKey == nil || *existing.SourceActionKey != *requested.SourceActionKey {
		return false
	}
	if existing.AuthorType == store.ActorTypeHuman {
		return existing.SourceRunID == nil && requested.SourceRunID == nil
	}
	return existing.SourceRunID != nil && requested.SourceRunID != nil && *existing.SourceRunID == *requested.SourceRunID
}

func sameIssueCommentMentionTargets(existing []store.IssueCommentMention, requested []string) bool {
	if len(existing) != len(requested) {
		return false
	}
	for index := range existing {
		if existing[index].TargetAgentID != requested[index] {
			return false
		}
	}
	return true
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
		&value.SourceRunID,
		&value.SourceActionKey,
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
