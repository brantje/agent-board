package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) PreviewIssueCommentTriggers(
	ctx context.Context,
	projectID, issueID string,
	parentCommentID *string,
	body string,
	request store.IssueCommentTriggerRequest,
) (store.IssueCommentTriggerPreview, error) {
	projectID = strings.TrimSpace(projectID)
	issueID = strings.TrimSpace(issueID)
	mentions, err := normalizeIssueCommentMentionAgentIDs(request.MentionAgentIDs)
	if err != nil || projectID == "" || issueID == "" {
		return store.IssueCommentTriggerPreview{}, store.ErrInvalidArgument
	}
	if parentCommentID != nil && strings.TrimSpace(*parentCommentID) == "" {
		return store.IssueCommentTriggerPreview{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueCommentTriggerPreview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	route, status, found, err := s.resolveIssueCommentImplicitRouteTx(
		ctx, tx, projectID, issueID, parentCommentID, len(mentions) != 0, false,
	)
	if err != nil {
		return store.IssueCommentTriggerPreview{}, err
	}

	result := store.IssueCommentTriggerPreview{
		Mentions: make([]store.IssueCommentMentionPreview, 0, len(mentions)),
	}
	for _, targetAgentID := range mentions {
		preview, err := s.issueCommentMentionTargetTx(ctx, tx, projectID, issueID, targetAgentID)
		if err != nil {
			return store.IssueCommentTriggerPreview{}, err
		}
		result.Mentions = append(result.Mentions, preview)
	}
	if len(mentions) != 0 || !found {
		return result, nil
	}

	target, err := s.issueCommentMentionTargetTx(ctx, tx, projectID, issueID, route.TargetAgentID)
	if err != nil {
		return store.IssueCommentTriggerPreview{}, err
	}
	implicit := &store.IssueCommentImplicitTriggerPreview{
		TargetAgentID: route.TargetAgentID, TargetAgentName: target.TargetAgentName, RoutingReason: route.RoutingReason,
	}
	switch {
	case request.SuppressImplicit:
		implicit.Suppressed = true
	case !store.IssueCommentImplicitWakeAllowed(status):
		reason := store.IssueCommentImplicitReasonWorkflowBlocked
		implicit.ReasonCode = &reason
	case !target.Eligible:
		implicit.ReasonCode = target.ReasonCode
	case strings.TrimSpace(body) != "" && utf8.RuneCountInString(body) > store.MaxDelegationTaskCharacters:
		reason := store.IssueCommentMentionReasonDelegationBlocked
		implicit.ReasonCode = &reason
	default:
		implicit.Eligible = true
	}
	result.Implicit = implicit
	return result, nil
}

func (s *Store) createIssueCommentImplicitTriggerTx(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
	comment store.IssueComment,
	suppress bool,
) (*store.IssueCommentImplicitTrigger, []store.Event, error) {
	route, status, found, err := s.resolveIssueCommentImplicitRouteTx(
		ctx, tx, projectID, comment.IssueID, comment.ParentCommentID, false, true,
	)
	if err != nil || !found {
		return nil, nil, err
	}

	target, err := s.issueCommentMentionTargetTx(ctx, tx, projectID, comment.IssueID, route.TargetAgentID)
	if err != nil {
		return nil, nil, err
	}

	var delegationID *string
	var delegatedRunID *string
	var reasonCode *string
	var events []store.Event
	outcome := store.IssueCommentImplicitOutcomeBlocked
	switch {
	case suppress:
		outcome = store.IssueCommentImplicitOutcomeSuppressed
	case !store.IssueCommentImplicitWakeAllowed(status):
		reason := store.IssueCommentImplicitReasonWorkflowBlocked
		reasonCode = &reason
	case !target.Eligible:
		reasonCode = target.ReasonCode
	case utf8.RuneCountInString(comment.Body) > store.MaxDelegationTaskCharacters:
		reason := store.IssueCommentMentionReasonDelegationBlocked
		reasonCode = &reason
	default:
		delegation, requestErr := s.requestIssueDelegationTx(ctx, tx, store.RequestIssueDelegationCommand{
			ProjectID:       projectID,
			IssueID:         comment.IssueID,
			SourceCommentID: comment.ID,
			TargetAgentID:   route.TargetAgentID,
			Task:            comment.Body,
			RequestKey:      issueCommentImplicitRequestKey(comment.ID, route.TargetAgentID),
		})
		if requestErr == nil {
			outcome = store.IssueCommentImplicitOutcomeQueued
			delegationID = &delegation.Delegation.ID
			delegatedRunID = &delegation.DelegatedRun.ID
			events = append(events, delegation.Events...)
		} else if errors.Is(requestErr, store.ErrConflict) || errors.Is(requestErr, store.ErrNotFound) || errors.Is(requestErr, store.ErrInvalidArgument) {
			reason := store.IssueCommentMentionReasonDelegationBlocked
			reasonCode = &reason
		} else {
			return nil, nil, requestErr
		}
	}

	var value store.IssueCommentImplicitTrigger
	if err := tx.QueryRow(ctx, `
		INSERT INTO issue_comment_implicit_triggers (
			project_id, issue_id, comment_id, target_agent_id, routing_reason, outcome, reason_code, delegation_id
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING target_agent_id::text, routing_reason, outcome, reason_code, delegation_id::text, created_at
	`, projectID, comment.IssueID, comment.ID, route.TargetAgentID, route.RoutingReason, outcome, reasonCode, delegationID).Scan(
		&value.TargetAgentID, &value.RoutingReason, &value.Outcome, &value.ReasonCode, &value.DelegationID, &value.CreatedAt,
	); err != nil {
		return nil, nil, err
	}
	value.TargetAgentName = target.TargetAgentName
	value.DelegatedRunID = delegatedRunID
	return &value, events, nil
}

func (s *Store) resolveIssueCommentImplicitRouteTx(
	ctx context.Context,
	tx pgx.Tx,
	projectID, issueID string,
	parentCommentID *string,
	hasExplicitMentions bool,
	lockIssue bool,
) (store.IssueCommentImplicitRoute, string, bool, error) {
	query := `SELECT status, assignee_type, assignee_id::text FROM issues WHERE project_id=$1 AND id=$2`
	if lockIssue {
		query += " FOR UPDATE"
	}
	var status string
	var assigneeType, assigneeID *string
	if err := tx.QueryRow(ctx, query, projectID, issueID).Scan(&status, &assigneeType, &assigneeID); err != nil {
		return store.IssueCommentImplicitRoute{}, "", false, notFound(err)
	}

	facts := store.IssueCommentImplicitRoutingFacts{
		HasExplicitMentions: hasExplicitMentions,
		AssigneeType:        assigneeType,
		AssigneeID:          assigneeID,
	}
	if parentCommentID == nil {
		route, found := store.ResolveIssueCommentImplicitRoute(facts)
		return route, status, found, nil
	}

	facts.IsReply = true
	if err := tx.QueryRow(ctx, `
		SELECT author_type, author_id::text
		FROM issue_comments
		WHERE issue_id=$1 AND id=$2 AND deleted_at IS NULL
		FOR KEY SHARE
	`, issueID, *parentCommentID).Scan(&facts.ParentAuthorType, &facts.ParentAuthorID); err != nil {
		return store.IssueCommentImplicitRoute{}, "", false, notFound(err)
	}
	if hasExplicitMentions || facts.ParentAuthorType == store.ActorTypeAgent {
		route, found := store.ResolveIssueCommentImplicitRoute(facts)
		return route, status, found, nil
	}

	chain, truncated, err := issueCommentAncestorIDsBoundedWith(
		ctx, tx, projectID, issueID, *parentCommentID, issueDiscussionRootTraversalDepth,
	)
	if err != nil {
		return store.IssueCommentImplicitRoute{}, "", false, err
	}
	if truncated || len(chain) == 0 {
		return store.IssueCommentImplicitRoute{}, status, false, nil
	}
	agents, truncated, err := issueCommentThreadAgentIDsTx(ctx, tx, issueID, chain[0], issueDiscussionRootTraversalDepth)
	if err != nil {
		return store.IssueCommentImplicitRoute{}, "", false, err
	}
	if truncated {
		return store.IssueCommentImplicitRoute{}, status, false, nil
	}
	facts.ThreadAgentIDs = agents
	route, found := store.ResolveIssueCommentImplicitRoute(facts)
	return route, status, found, nil
}

func issueCommentThreadAgentIDsTx(ctx context.Context, tx pgx.Tx, issueID, rootID string, maxDepth int) ([]string, bool, error) {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE thread(id, author_type, author_id, depth) AS (
			SELECT id, author_type, author_id, 0
			FROM issue_comments
			WHERE issue_id=$1 AND id=$2
			UNION ALL
			SELECT child.id, child.author_type, child.author_id, thread.depth + 1
			FROM thread
			JOIN issue_comments AS child
			  ON child.issue_id=$1 AND child.parent_comment_id=thread.id
			WHERE thread.depth < $3
		),
		participants AS (
			SELECT DISTINCT author_id
			FROM thread
			WHERE author_type='AGENT'
		),
		overflow AS (
			SELECT EXISTS (
				SELECT 1
				FROM thread
				JOIN issue_comments AS child
				  ON child.issue_id=$1 AND child.parent_comment_id=thread.id
				WHERE thread.depth=$3
			) AS value
		)
		SELECT participants.author_id::text, overflow.value
		FROM overflow
		LEFT JOIN participants ON true
		ORDER BY participants.author_id
		LIMIT 3
	`, issueID, rootID, maxDepth)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	agents := make([]string, 0, 2)
	truncated := false
	for rows.Next() {
		var agentID *string
		if err := rows.Scan(&agentID, &truncated); err != nil {
			return nil, false, err
		}
		if agentID != nil {
			agents = append(agents, *agentID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return agents, truncated, nil
}

func issueCommentImplicitRequestKey(commentID, targetAgentID string) string {
	return fmt.Sprintf("comment:%s:implicit:%s", commentID, targetAgentID)
}

func sameIssueCommentImplicitRequest(existing store.IssueComment, enabled, suppress, persistedSuppress, hasExplicitMentions bool) bool {
	if !enabled || hasExplicitMentions {
		return existing.ImplicitTrigger == nil && !persistedSuppress
	}
	if persistedSuppress != suppress {
		return false
	}
	if existing.ImplicitTrigger == nil {
		return true
	}
	if persistedSuppress {
		return existing.ImplicitTrigger.Outcome == store.IssueCommentImplicitOutcomeSuppressed
	}
	return existing.ImplicitTrigger.Outcome != store.IssueCommentImplicitOutcomeSuppressed
}

func (s *Store) loadIssueCommentImplicitTriggers(ctx context.Context, projectID, issueID string, values []store.IssueComment) error {
	return loadIssueCommentImplicitTriggersWith(ctx, s.pool, projectID, issueID, values)
}

func loadIssueCommentImplicitTriggersWith(ctx context.Context, q issueCommentRowsQuerier, projectID, issueID string, values []store.IssueComment) error {
	if len(values) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `
		SELECT t.comment_id::text, t.target_agent_id::text, COALESCE(a.name, ''),
		       t.routing_reason, t.outcome, t.reason_code, t.delegation_id::text,
		       d.delegated_run_id::text, t.created_at
		FROM issue_comment_implicit_triggers AS t
		LEFT JOIN agents AS a
		  ON a.id=t.target_agent_id
		 AND (a.project_id IS NULL OR a.project_id=t.project_id)
		LEFT JOIN delegations AS d
		  ON d.project_id=t.project_id
		 AND d.id=t.delegation_id
		WHERE t.project_id=$1 AND t.issue_id=$2
		ORDER BY t.comment_id
	`, projectID, issueID)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[string]int, len(values))
	for index := range values {
		byID[values[index].ID] = index
		values[index].ImplicitTrigger = nil
	}
	for rows.Next() {
		var commentID string
		var trigger store.IssueCommentImplicitTrigger
		if err := rows.Scan(
			&commentID, &trigger.TargetAgentID, &trigger.TargetAgentName,
			&trigger.RoutingReason, &trigger.Outcome, &trigger.ReasonCode,
			&trigger.DelegationID, &trigger.DelegatedRunID, &trigger.CreatedAt,
		); err != nil {
			return err
		}
		index, ok := byID[commentID]
		if !ok {
			continue
		}
		values[index].ImplicitTrigger = &trigger
	}
	return rows.Err()
}
