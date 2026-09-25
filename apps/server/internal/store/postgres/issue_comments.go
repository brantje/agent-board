package postgres

import (
	"context"
	"encoding/json"
	"errors"
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
	if err := s.loadIssueCommentImplicitTriggers(ctx, projectID, issueID, values); err != nil {
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
	if err := s.loadIssueCommentImplicitTriggers(ctx, projectID, issueID, values); err != nil {
		return store.IssueComment{}, err
	}
	return values[0], nil
}

func (s *Store) CreateIssueComment(ctx context.Context, projectID string, input store.IssueComment) (store.IssueCommentMutationResult, error) {
	return s.createIssueComment(ctx, projectID, input, nil, false, false)
}

func (s *Store) CreateIssueCommentWithMentions(ctx context.Context, projectID string, input store.IssueComment, mentionAgentIDs []string) (store.IssueCommentMutationResult, error) {
	targets := make([]store.IssueCommentTarget, 0, len(mentionAgentIDs))
	for _, id := range mentionAgentIDs {
		targets = append(targets, store.IssueCommentTarget{Type: store.IssueCommentTargetTypeAgent, ID: id})
	}
	return s.createIssueComment(ctx, projectID, input, targets, false, false)
}

func (s *Store) CreateIssueCommentWithTargets(ctx context.Context, projectID string, input store.IssueComment, targets []store.IssueCommentTarget) (store.IssueCommentMutationResult, error) {
	return s.createIssueComment(ctx, projectID, input, targets, false, false)
}

func (s *Store) CreateIssueCommentWithTriggers(ctx context.Context, projectID string, input store.IssueComment, request store.IssueCommentTriggerRequest) (store.IssueCommentMutationResult, error) {
	targets, err := normalizeIssueCommentTargets(request.MentionTargets, request.MentionAgentIDs)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	return s.createIssueComment(ctx, projectID, input, targets, true, request.SuppressImplicit)
}

func (s *Store) PreviewIssueCommentMentions(ctx context.Context, projectID, issueID string, mentionAgentIDs []string) ([]store.IssueCommentMentionPreview, error) {
	targets := make([]store.IssueCommentTarget, 0, len(mentionAgentIDs))
	for _, id := range mentionAgentIDs {
		targets = append(targets, store.IssueCommentTarget{Type: store.IssueCommentTargetTypeAgent, ID: id})
	}
	return s.PreviewIssueCommentTargets(ctx, projectID, issueID, targets)
}

func (s *Store) PreviewIssueCommentTargets(ctx context.Context, projectID, issueID string, targets []store.IssueCommentTarget) ([]store.IssueCommentMentionPreview, error) {
	projectID = strings.TrimSpace(projectID)
	issueID = strings.TrimSpace(issueID)
	mentions, err := normalizeIssueCommentTargets(targets, nil)
	if err != nil || projectID == "" || issueID == "" {
		return nil, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM issues WHERE project_id=$1 AND id=$2)
	`, projectID, issueID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, store.ErrNotFound
	}
	result := make([]store.IssueCommentMentionPreview, 0, len(mentions))
	for _, target := range mentions {
		preview, err := s.issueCommentTargetTx(ctx, tx, projectID, issueID, target)
		if err != nil {
			return nil, err
		}
		result = append(result, preview)
	}
	return result, nil
}

func (s *Store) createIssueComment(ctx context.Context, projectID string, input store.IssueComment, mentions []store.IssueCommentTarget, implicitRouting, suppressImplicit bool) (store.IssueCommentMutationResult, error) {
	var err error
	mentions, err = normalizeIssueCommentTargets(mentions, nil)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(input.IssueID) == "" || strings.TrimSpace(input.AuthorID) == "" || strings.TrimSpace(input.Body) == "" || !store.ValidActorType(input.AuthorType) {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	if input.ParentCommentID != nil && strings.TrimSpace(*input.ParentCommentID) == "" {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	persistedSuppressImplicit := implicitRouting && len(mentions) == 0 && suppressImplicit
	if input.SourceActionKey != nil {
		value := strings.TrimSpace(*input.SourceActionKey)
		if value == "" {
			return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
		}
		input.SourceActionKey = &value
	}
	if (len(mentions) != 0 || implicitRouting) && input.SourceActionKey == nil {
		return store.IssueCommentMutationResult{}, store.ErrInvalidArgument
	}
	if implicitRouting && input.AuthorType != store.ActorTypeHuman {
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
		INSERT INTO issue_comments (
			issue_id, parent_comment_id, author_type, author_id, source_run_id, source_action_key, body,
			suppress_implicit_agent_trigger
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT DO NOTHING
		RETURNING id::text, issue_id::text, parent_comment_id::text, author_type, author_id::text,
		          $9::text, source_run_id::text, source_action_key, COALESCE(body, ''), deleted_at, resolved_at, resolved_by_user_id::text,
		          ''::text, created_at, updated_at
	`, issueID, input.ParentCommentID, input.AuthorType, input.AuthorID, input.SourceRunID, input.SourceActionKey, input.Body, persistedSuppressImplicit, authorName))
	if errors.Is(err, store.ErrNotFound) && input.SourceActionKey != nil {
		existing, lookupErr := existingIssueCommentRequest(ctx, tx, issueID, authorName, input)
		if lookupErr != nil {
			return store.IssueCommentMutationResult{}, lookupErr
		}
		if !sameIssueCommentRequest(existing, input) {
			return store.IssueCommentMutationResult{}, store.ErrConflict
		}
		existingSuppressImplicit, lookupErr := existingIssueCommentSuppressionIntent(ctx, tx, issueID, existing.ID)
		if lookupErr != nil {
			return store.IssueCommentMutationResult{}, lookupErr
		}
		values := []store.IssueComment{existing}
		if err := loadIssueCommentMentionsWith(ctx, tx, projectID, issueID, values); err != nil {
			return store.IssueCommentMutationResult{}, err
		}
		if err := loadIssueCommentImplicitTriggersWith(ctx, tx, projectID, issueID, values); err != nil {
			return store.IssueCommentMutationResult{}, err
		}
		if !sameIssueCommentMentionTargets(values[0].Mentions, mentions) ||
			!sameIssueCommentImplicitRequest(values[0], implicitRouting, suppressImplicit, existingSuppressImplicit, len(mentions) != 0) {
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

	mentionValues, delegationEvents, err := s.createIssueCommentMentionsTx(ctx, tx, projectID, comment, mentions)
	if err != nil {
		return store.IssueCommentMutationResult{}, err
	}
	comment.Mentions = mentionValues

	if implicitRouting && len(mentions) == 0 {
		implicit, implicitEvents, err := s.createIssueCommentImplicitTriggerTx(ctx, tx, projectID, comment, suppressImplicit)
		if err != nil {
			return store.IssueCommentMutationResult{}, err
		}
		comment.ImplicitTrigger = implicit
		delegationEvents = append(delegationEvents, implicitEvents...)
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

func existingIssueCommentSuppressionIntent(ctx context.Context, tx pgx.Tx, issueID, commentID string) (bool, error) {
	var suppressed bool
	if err := tx.QueryRow(ctx, `
		SELECT suppress_implicit_agent_trigger
		FROM issue_comments
		WHERE issue_id=$1 AND id=$2
	`, issueID, commentID).Scan(&suppressed); err != nil {
		return false, notFound(err)
	}
	return suppressed, nil
}

func (s *Store) createIssueCommentMentionsTx(ctx context.Context, tx pgx.Tx, projectID string, comment store.IssueComment, targets []store.IssueCommentTarget) ([]store.IssueCommentMention, []store.Event, error) {
	if len(targets) == 0 {
		return nil, nil, nil
	}
	result := make([]store.IssueCommentMention, 0, len(targets))
	events := make([]store.Event, 0, len(targets))
	for index, target := range targets {
		preview, err := s.issueCommentTargetTx(ctx, tx, projectID, comment.IssueID, target)
		if err != nil {
			return nil, nil, err
		}

		var persistedDelegationID *string
		var workRequest *store.AgentWorkRequest
		var reasonCode *string
		outcome := store.IssueCommentMentionOutcomeBlocked
		if !preview.Eligible {
			reasonCode = preview.ReasonCode
		} else if utf8.RuneCountInString(comment.Body) > store.MaxDelegationTaskCharacters {
			reason := store.IssueCommentMentionReasonDelegationBlocked
			reasonCode = &reason
		} else {
			authority := store.AgentWorkRequestAuthorityIssue
			var parentRunID *string
			if comment.AuthorType == store.ActorTypeAgent {
				authority = store.AgentWorkRequestAuthorityParentRun
				parentRunID = comment.SourceRunID
			}
			requested, requestErr := s.requestAgentWorkTx(ctx, tx, agentWorkRequestInput{
				ProjectID: projectID, IssueID: comment.IssueID, SourceCommentID: comment.ID,
				TargetAgentID: preview.ResolvedAgentID, Task: comment.Body, AuthorityKind: authority, ParentRunID: parentRunID,
			})
			if requestErr == nil {
				outcome = requested.Outcome
				workRequest = &requested.WorkRequest
				if requested.Outcome == agentWorkRequestOutcomeQueued {
					persistedDelegationID = requested.WorkRequest.DelegationID
				}
				events = append(events, requested.Events...)
			} else if errors.Is(requestErr, store.ErrConflict) || errors.Is(requestErr, store.ErrNotFound) || errors.Is(requestErr, store.ErrInvalidArgument) {
				reason := store.IssueCommentMentionReasonDelegationBlocked
				reasonCode = &reason
			} else {
				return nil, nil, requestErr
			}
		}

		var workRequestID *string
		if workRequest != nil {
			workRequestID = &workRequest.ID
		}
		var mention store.IssueCommentMention
		storedAgentID := preview.ResolvedAgentID
		if storedAgentID == "" {
			storedAgentID = target.ID
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO issue_comment_mentions (
				project_id, issue_id, comment_id, ordinal, target_agent_id, target_type, target_id, resolved_agent_id, outcome, reason_code, delegation_id, work_request_id
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			RETURNING id::text, target_agent_id::text, target_type, target_id::text, COALESCE(resolved_agent_id::text, ''), outcome, reason_code, delegation_id::text, work_request_id::text, created_at
		`, projectID, comment.IssueID, comment.ID, index, storedAgentID, target.Type, target.ID, nullableString(preview.ResolvedAgentID), outcome, reasonCode, persistedDelegationID, workRequestID).Scan(
			&mention.ID, &mention.TargetAgentID, &mention.Target.Type, &mention.Target.ID, &mention.ResolvedAgentID, &mention.Outcome, &mention.ReasonCode, &mention.DelegationID, &mention.WorkRequestID, &mention.CreatedAt,
		); err != nil {
			return nil, nil, err
		}
		mention.Target = target
		mention.TargetName = preview.TargetName
		mention.ResolvedAgentName = preview.ResolvedAgentName
		mention.TargetAgentID = storedAgentID
		mention.TargetAgentName = preview.ResolvedAgentName
		if workRequest != nil && workRequest.DelegationID != nil {
			mention.DelegationID = workRequest.DelegationID
			mention.DelegatedRunID = workRequest.RunID
		}
		result = append(result, mention)
	}
	return result, events, nil
}

func (s *Store) issueCommentMentionTargetTx(ctx context.Context, tx pgx.Tx, projectID, issueID, targetAgentID string) (store.IssueCommentMentionPreview, error) {
	preview := store.IssueCommentMentionPreview{Target: store.IssueCommentTarget{Type: store.IssueCommentTargetTypeAgent, ID: targetAgentID}, TargetAgentID: targetAgentID}
	if err := tx.QueryRow(ctx, `
		SELECT name
		FROM agents
		WHERE id=$2 AND (project_id IS NULL OR project_id=$1)
		FOR SHARE
	`, projectID, targetAgentID).Scan(&preview.TargetAgentName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			reason := store.IssueCommentMentionReasonTargetUnavailable
			preview.ReasonCode = &reason
			return preview, nil
		}
		return store.IssueCommentMentionPreview{}, err
	}
	preview.TargetName = preview.TargetAgentName
	preview.ResolvedAgentID = targetAgentID
	preview.ResolvedAgentName = preview.TargetAgentName
	if err := s.verifyRunnableAgent(ctx, tx, projectID, targetAgentID); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
			reason := store.IssueCommentMentionReasonTargetUnavailable
			preview.ReasonCode = &reason
			return preview, nil
		}
		return store.IssueCommentMentionPreview{}, err
	}
	preview.Eligible = true
	return preview, nil
}

func (s *Store) issueCommentTargetTx(ctx context.Context, tx pgx.Tx, projectID, issueID string, target store.IssueCommentTarget) (store.IssueCommentMentionPreview, error) {
	if target.Type == store.IssueCommentTargetTypeAgent {
		return s.issueCommentMentionTargetTx(ctx, tx, projectID, issueID, target.ID)
	}
	if target.Type != store.IssueCommentTargetTypeSquad {
		return store.IssueCommentMentionPreview{}, store.ErrInvalidArgument
	}
	preview := store.IssueCommentMentionPreview{Target: target}
	if err := tx.QueryRow(ctx, `
		SELECT name, leader_agent_id::text
		FROM squads
		WHERE project_id=$1 AND id=$2
		FOR SHARE
	`, projectID, target.ID).Scan(&preview.TargetName, &preview.ResolvedAgentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			reason := store.IssueCommentMentionReasonTargetUnavailable
			preview.ReasonCode = &reason
			return preview, nil
		}
		return store.IssueCommentMentionPreview{}, err
	}
	preview.TargetAgentID = preview.ResolvedAgentID
	preview.TargetAgentName = preview.ResolvedAgentName
	agentPreview, err := s.issueCommentMentionTargetTx(ctx, tx, projectID, issueID, preview.ResolvedAgentID)
	if err != nil {
		return store.IssueCommentMentionPreview{}, err
	}
	preview.Eligible = agentPreview.Eligible
	preview.ReasonCode = agentPreview.ReasonCode
	preview.ResolvedAgentName = agentPreview.TargetAgentName
	preview.TargetAgentName = preview.ResolvedAgentName
	return preview, nil
}

func normalizeIssueCommentTargets(values []store.IssueCommentTarget, legacyAgentIDs []string) ([]store.IssueCommentTarget, error) {
	if len(values) == 0 && len(legacyAgentIDs) != 0 {
		values = make([]store.IssueCommentTarget, 0, len(legacyAgentIDs))
		for _, id := range legacyAgentIDs {
			values = append(values, store.IssueCommentTarget{Type: store.IssueCommentTargetTypeAgent, ID: id})
		}
	}
	if len(values) > store.MaxIssueCommentMentions {
		return nil, store.ErrInvalidArgument
	}
	result := make([]store.IssueCommentTarget, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, target := range values {
		target.Type = strings.TrimSpace(target.Type)
		target.ID = strings.TrimSpace(target.ID)
		if !target.Valid() || !validUUIDText(target.ID) {
			return nil, store.ErrInvalidArgument
		}
		key := target.Type + ":" + target.ID
		if _, duplicate := seen[key]; duplicate {
			return nil, store.ErrInvalidArgument
		}
		seen[key] = struct{}{}
		result = append(result, target)
	}
	return result, nil
}

func nullableString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
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
		) OR EXISTS (
			SELECT 1 FROM issue_comment_implicit_triggers
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
		SELECT m.comment_id::text, m.id::text, m.target_agent_id::text, m.target_type, m.target_id::text,
		       COALESCE(CASE WHEN m.target_type='SQUAD' THEN s.name ELSE a.name END, ''),
		       COALESCE(m.resolved_agent_id::text, ''), COALESCE(ra.name, a.name, ''), m.outcome, m.reason_code,
		       COALESCE(m.delegation_id, wr.delegation_id)::text, m.work_request_id::text,
		       d.delegated_run_id::text, m.created_at
		FROM issue_comment_mentions AS m
		LEFT JOIN agents AS a
		  ON a.id=m.target_agent_id
		 AND (a.project_id IS NULL OR a.project_id=m.project_id)
		LEFT JOIN squads AS s ON s.project_id=m.project_id AND s.id=m.target_id
		LEFT JOIN agents AS ra
		  ON ra.id=m.resolved_agent_id
		 AND (ra.project_id IS NULL OR ra.project_id=m.project_id)
		LEFT JOIN agent_work_requests AS wr
		  ON wr.project_id=m.project_id
		 AND wr.id=m.work_request_id
		LEFT JOIN delegations AS d
		  ON d.project_id=m.project_id
		 AND d.id=COALESCE(m.delegation_id, wr.delegation_id)
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
			&commentID, &mention.ID, &mention.TargetAgentID, &mention.Target.Type, &mention.Target.ID, &mention.TargetName,
			&mention.ResolvedAgentID, &mention.ResolvedAgentName, &mention.Outcome, &mention.ReasonCode, &mention.DelegationID, &mention.WorkRequestID, &mention.DelegatedRunID, &mention.CreatedAt,
		); err != nil {
			return err
		}
		index, ok := byID[commentID]
		if !ok {
			continue
		}
		mention.TargetAgentName = mention.ResolvedAgentName
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

func sameIssueCommentMentionTargets(existing []store.IssueCommentMention, requested []store.IssueCommentTarget) bool {
	if len(existing) != len(requested) {
		return false
	}
	for index := range existing {
		if existing[index].Target.Type != requested[index].Type || existing[index].Target.ID != requested[index].ID {
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
