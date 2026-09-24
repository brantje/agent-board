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

const (
	agentWorkRequestOutcomeQueued    = "QUEUED"
	agentWorkRequestOutcomeCoalesced = "COALESCED"
	agentWorkRequestOutcomeDeferred  = "DEFERRED"
)

type agentWorkRequestInput struct {
	ProjectID       string
	IssueID         string
	SourceCommentID string
	TargetAgentID   string
	Task            string
	AuthorityKind   string
	ParentRunID     *string
}

type agentWorkRequestResult struct {
	WorkRequest store.AgentWorkRequest
	Outcome     string
	Delegation  *store.RequestDelegationResult
	Events      []store.Event
}

const agentWorkRequestSelectColumns = `
	id::text,
	project_id::text,
	issue_id::text,
	workspace_id::text,
	target_agent_id::text,
	authority_kind,
	parent_run_id::text,
	run_id::text,
	delegation_id::text,
	sealed_at,
	closed_at,
	created_at,
	updated_at
`

// requestAgentWorkTx is the single transaction-local admission/coalescing
// policy for comment-triggered Agent work. It decides whether a trigger creates
// canonical delegation work, joins a compatible queued/pending request, or is
// durably deferred behind active execution. It never injects input into a live
// Engine session.
func (s *Store) requestAgentWorkTx(ctx context.Context, tx pgx.Tx, input agentWorkRequestInput) (agentWorkRequestResult, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.IssueID = strings.TrimSpace(input.IssueID)
	input.SourceCommentID = strings.TrimSpace(input.SourceCommentID)
	input.TargetAgentID = strings.TrimSpace(input.TargetAgentID)
	input.Task = strings.TrimSpace(input.Task)
	if input.ParentRunID != nil {
		value := strings.TrimSpace(*input.ParentRunID)
		input.ParentRunID = &value
	}
	if input.ProjectID == "" || input.IssueID == "" || input.SourceCommentID == "" || input.TargetAgentID == "" || input.Task == "" || utf8.RuneCountInString(input.Task) > store.MaxDelegationTaskCharacters {
		return agentWorkRequestResult{}, store.ErrInvalidArgument
	}
	if input.AuthorityKind != store.AgentWorkRequestAuthorityIssue && input.AuthorityKind != store.AgentWorkRequestAuthorityParentRun {
		return agentWorkRequestResult{}, store.ErrInvalidArgument
	}
	if (input.AuthorityKind == store.AgentWorkRequestAuthorityIssue) != (input.ParentRunID == nil) || (input.ParentRunID != nil && *input.ParentRunID == "") {
		return agentWorkRequestResult{}, store.ErrInvalidArgument
	}

	var parent *store.Run
	if input.ParentRunID != nil {
		value, err := lockRunForAgentWork(ctx, tx, input.ProjectID, *input.ParentRunID)
		if err != nil {
			return agentWorkRequestResult{}, err
		}
		if value.IssueID != input.IssueID {
			return agentWorkRequestResult{}, store.ErrConflict
		}
		parent = &value
	}

	issue, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, input.ProjectID, input.IssueID)
	if err != nil {
		return agentWorkRequestResult{}, err
	}
	workspace, err := workspaceForAssignment(ctx, tx, input.ProjectID, issue.ID, issue.Key, repositoryPath, defaultBranch)
	if err != nil {
		return agentWorkRequestResult{}, err
	}
	if parent != nil && parent.WorkspaceID != workspace.ID {
		return agentWorkRequestResult{}, store.ErrConflict
	}
	if err := validateAgentWorkSourceComment(ctx, tx, input, parent); err != nil {
		return agentWorkRequestResult{}, err
	}
	if err := s.verifyRunnableAgent(ctx, tx, input.ProjectID, input.TargetAgentID); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
			return agentWorkRequestResult{}, store.ErrConflict
		}
		return agentWorkRequestResult{}, err
	}

	existing, found, err := findOpenAgentWorkRequestTx(ctx, tx, input, workspace.ID)
	if err != nil {
		return agentWorkRequestResult{}, err
	}
	if found {
		if existing.RunID == nil {
			return agentWorkRequestResult{WorkRequest: existing, Outcome: agentWorkRequestOutcomeCoalesced}, nil
		}
		run, err := lockRunForAgentWork(ctx, tx, input.ProjectID, *existing.RunID)
		if err != nil {
			return agentWorkRequestResult{}, err
		}
		if run.Status == "QUEUED" {
			return agentWorkRequestResult{WorkRequest: existing, Outcome: agentWorkRequestOutcomeCoalesced}, nil
		}
		if err := sealAgentWorkRequestTx(ctx, tx, existing.ID); err != nil {
			return agentWorkRequestResult{}, err
		}
	}

	active, err := lockActiveRunForAgentWork(ctx, tx, input.ProjectID, input.IssueID, input.TargetAgentID)
	if err == nil {
		if active.Status == "QUEUED" {
			delegationID, compatible, compatErr := queuedRunAgentWorkCompatibility(ctx, tx, input, active)
			if compatErr != nil {
				return agentWorkRequestResult{}, compatErr
			}
			if compatible {
				request, err := insertAgentWorkRequestTx(ctx, tx, input, workspace.ID, &active.ID, delegationID)
				if err != nil {
					return agentWorkRequestResult{}, err
				}
				return agentWorkRequestResult{WorkRequest: request, Outcome: agentWorkRequestOutcomeCoalesced}, nil
			}
		}
		// Parent-Run authority cannot be silently reinterpreted as Issue-origin
		// follow-up work. Without a compatible queued delegation, there is no
		// later native handoff boundary that can safely pause the parent Engine
		// session, so the canonical delegation policy remains authoritative.
		if input.AuthorityKind == store.AgentWorkRequestAuthorityParentRun {
			return agentWorkRequestResult{}, store.ErrConflict
		}
		request, err := insertAgentWorkRequestTx(ctx, tx, input, workspace.ID, nil, nil)
		if err != nil {
			return agentWorkRequestResult{}, err
		}
		return agentWorkRequestResult{WorkRequest: request, Outcome: agentWorkRequestOutcomeDeferred}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return agentWorkRequestResult{}, err
	}

	var requestID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&requestID); err != nil {
		return agentWorkRequestResult{}, err
	}
	requestKey := agentWorkRequestDelegationKey(requestID)
	var delegation store.RequestDelegationResult
	if input.AuthorityKind == store.AgentWorkRequestAuthorityParentRun {
		delegation, err = s.requestDelegationTx(ctx, tx, store.RequestDelegationCommand{
			ProjectID: input.ProjectID, ParentRunID: *input.ParentRunID, TargetAgentID: input.TargetAgentID,
			Task: input.Task, RequestKey: requestKey,
		})
	} else {
		delegation, err = s.requestIssueDelegationTx(ctx, tx, store.RequestIssueDelegationCommand{
			ProjectID: input.ProjectID, IssueID: input.IssueID, SourceCommentID: input.SourceCommentID,
			TargetAgentID: input.TargetAgentID, Task: input.Task, RequestKey: requestKey,
		})
	}
	if err != nil {
		return agentWorkRequestResult{}, err
	}
	request, err := insertAgentWorkRequestWithIDTx(ctx, tx, requestID, input, workspace.ID, &delegation.DelegatedRun.ID, &delegation.Delegation.ID)
	if err != nil {
		return agentWorkRequestResult{}, err
	}
	return agentWorkRequestResult{
		WorkRequest: request,
		Outcome:     agentWorkRequestOutcomeQueued,
		Delegation:  &delegation,
		Events:      delegation.Events,
	}, nil
}

func validateAgentWorkSourceComment(ctx context.Context, tx pgx.Tx, input agentWorkRequestInput, parent *store.Run) error {
	var authorType string
	var sourceRunID *string
	var body string
	if err := tx.QueryRow(ctx, `
		SELECT author_type, source_run_id::text, COALESCE(body, '')
		FROM issue_comments
		WHERE issue_id=$1 AND id=$2 AND deleted_at IS NULL
		FOR KEY SHARE
	`, input.IssueID, input.SourceCommentID).Scan(&authorType, &sourceRunID, &body); err != nil {
		return notFound(err)
	}
	if strings.TrimSpace(body) != input.Task {
		return store.ErrConflict
	}
	if input.AuthorityKind == store.AgentWorkRequestAuthorityIssue {
		if authorType != store.ActorTypeHuman || sourceRunID != nil {
			return store.ErrConflict
		}
		return nil
	}
	if parent == nil || authorType != store.ActorTypeAgent || sourceRunID == nil || *sourceRunID != parent.ID || parent.Status != "RUNNING" {
		return store.ErrConflict
	}
	return nil
}

func findOpenAgentWorkRequestTx(ctx context.Context, tx pgx.Tx, input agentWorkRequestInput, workspaceID string) (store.AgentWorkRequest, bool, error) {
	query := `SELECT ` + agentWorkRequestSelectColumns + ` FROM agent_work_requests
		WHERE project_id=$1 AND issue_id=$2 AND workspace_id=$3 AND target_agent_id=$4
		  AND authority_kind=$5 AND sealed_at IS NULL`
	args := []any{input.ProjectID, input.IssueID, workspaceID, input.TargetAgentID, input.AuthorityKind}
	if input.ParentRunID == nil {
		query += ` AND parent_run_id IS NULL`
	} else {
		query += ` AND parent_run_id=$6`
		args = append(args, *input.ParentRunID)
	}
	query += ` FOR UPDATE`
	value, err := scanAgentWorkRequest(tx.QueryRow(ctx, query, args...))
	if errors.Is(err, store.ErrNotFound) {
		return store.AgentWorkRequest{}, false, nil
	}
	if err != nil {
		return store.AgentWorkRequest{}, false, err
	}
	return value, true, nil
}

func insertAgentWorkRequestTx(ctx context.Context, tx pgx.Tx, input agentWorkRequestInput, workspaceID string, runID, delegationID *string) (store.AgentWorkRequest, error) {
	return insertAgentWorkRequestWithIDTx(ctx, tx, "", input, workspaceID, runID, delegationID)
}

func insertAgentWorkRequestWithIDTx(ctx context.Context, tx pgx.Tx, id string, input agentWorkRequestInput, workspaceID string, runID, delegationID *string) (store.AgentWorkRequest, error) {
	return scanAgentWorkRequest(tx.QueryRow(ctx, `
		INSERT INTO agent_work_requests (id, project_id, issue_id, workspace_id, target_agent_id, authority_kind, parent_run_id, run_id, delegation_id)
		VALUES (COALESCE(NULLIF($1, '')::uuid, gen_random_uuid()),$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+agentWorkRequestSelectColumns,
		id, input.ProjectID, input.IssueID, workspaceID, input.TargetAgentID, input.AuthorityKind, input.ParentRunID, runID, delegationID,
	))
}

func sealAgentWorkRequestTx(ctx context.Context, tx pgx.Tx, requestID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE agent_work_requests
		SET sealed_at=COALESCE(sealed_at, now()), updated_at=now()
		WHERE id=$1 AND sealed_at IS NULL
	`, requestID)
	return err
}

func lockRunForAgentWork(ctx context.Context, tx pgx.Tx, projectID, runID string) (store.Run, error) {
	return scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs WHERE project_id=$1 AND id=$2 FOR UPDATE
	`, projectID, runID))
}

func lockActiveRunForAgentWork(ctx context.Context, tx pgx.Tx, projectID, issueID, agentID string) (store.Run, error) {
	return scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND issue_id=$2 AND agent_id=$3 AND status = ANY($4::text[])
		ORDER BY attempt DESC
		LIMIT 1
		FOR UPDATE
	`, projectID, issueID, agentID, activeRunStatuses))
}

func queuedRunAgentWorkCompatibility(ctx context.Context, tx pgx.Tx, input agentWorkRequestInput, run store.Run) (*string, bool, error) {
	delegation, err := scanDelegation(tx.QueryRow(ctx, `
		SELECT `+delegationSelectColumns+`
		FROM delegations
		WHERE project_id=$1 AND delegated_run_id=$2
	`, input.ProjectID, run.ID))
	if errors.Is(err, store.ErrNotFound) {
		return nil, input.AuthorityKind == store.AgentWorkRequestAuthorityIssue, nil
	}
	if err != nil {
		return nil, false, err
	}
	if input.AuthorityKind == store.AgentWorkRequestAuthorityIssue {
		return &delegation.ID, delegation.SourceCommentID != nil && delegation.ParentRunID == "", nil
	}
	return &delegation.ID, input.ParentRunID != nil && delegation.ParentRunID == *input.ParentRunID, nil
}

// GetAgentWorkRequestExecutionContext resolves the durable comment inputs
// associated with one Run. The work request is the coalescing identity; Issue
// comments remain the authoritative collaboration content.
func (s *Store) GetAgentWorkRequestExecutionContext(ctx context.Context, projectID, runID string) (*store.AgentWorkRequestExecutionContext, error) {
	projectID = strings.TrimSpace(projectID)
	runID = strings.TrimSpace(runID)
	if projectID == "" || runID == "" {
		return nil, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	request, err := scanAgentWorkRequest(tx.QueryRow(ctx, `
		SELECT `+agentWorkRequestSelectColumns+`
		FROM agent_work_requests
		WHERE project_id=$1 AND run_id=$2 AND closed_at IS NULL
		FOR SHARE
	`, projectID, runID))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		WITH triggers AS (
			SELECT m.comment_id, 'MENTION'::text AS trigger_kind, NULL::text AS routing_reason
			FROM issue_comment_mentions AS m
			WHERE m.project_id=$1 AND m.issue_id=$2 AND m.work_request_id=$3
			UNION ALL
			SELECT t.comment_id, 'IMPLICIT'::text AS trigger_kind, t.routing_reason
			FROM issue_comment_implicit_triggers AS t
			WHERE t.project_id=$1 AND t.issue_id=$2 AND t.work_request_id=$3
		)
		SELECT c.id::text, c.author_type, c.author_id::text,
		       COALESCE(
		           CASE
		               WHEN c.author_type='HUMAN' THEN (SELECT u.display_name FROM users AS u WHERE u.id=c.author_id)
		               WHEN c.author_type='AGENT' THEN (SELECT a.name FROM agents AS a WHERE a.id=c.author_id AND (a.project_id IS NULL OR a.project_id=$1))
		           END,
		           ''
		       ),
		       COALESCE(c.body, ''), c.deleted_at IS NOT NULL, c.parent_comment_id::text,
		       triggers.trigger_kind, triggers.routing_reason, c.created_at
		FROM triggers
		JOIN issue_comments AS c ON c.issue_id=$2 AND c.id=triggers.comment_id
		ORDER BY c.created_at, c.id, triggers.trigger_kind
	`, projectID, request.IssueID, request.ID)
	if err != nil {
		return nil, err
	}

	comments := make([]store.AgentWorkRequestComment, 0)
	for rows.Next() {
		var comment store.AgentWorkRequestComment
		if err := rows.Scan(
			&comment.CommentID, &comment.AuthorType, &comment.AuthorID, &comment.AuthorName,
			&comment.Body, &comment.Deleted, &comment.ParentCommentID,
			&comment.TriggerKind, &comment.RoutingReason, &comment.CreatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(comments) == 0 {
		return nil, store.ErrConflict
	}

	for index := range comments {
		chain, truncated, err := issueCommentAncestorIDsBoundedWith(
			ctx, tx, projectID, request.IssueID, comments[index].CommentID, issueDiscussionRootTraversalDepth,
		)
		if err != nil {
			return nil, err
		}
		if !truncated && len(chain) != 0 {
			root := chain[0]
			comments[index].RootCommentID = &root
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &store.AgentWorkRequestExecutionContext{WorkRequestID: request.ID, Comments: comments}, nil
}

// ReconcilePendingAgentWorkRequest promotes at most one durable Issue-origin
// follow-up request through canonical delegation. It is called by the existing
// scheduler loop, so restart recovery does not need a comment-owned worker.
func (s *Store) ReconcilePendingAgentWorkRequest(ctx context.Context) (store.AgentWorkRequestReconciliationResult, error) {
	candidate, err := scanAgentWorkRequest(s.pool.QueryRow(ctx, `
		SELECT `+agentWorkRequestSelectColumns+`
		FROM agent_work_requests AS wr
		WHERE wr.authority_kind='ISSUE'
		  AND wr.run_id IS NULL
		  AND wr.delegation_id IS NULL
		  AND wr.sealed_at IS NULL
		  AND wr.closed_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM runs AS active
			WHERE active.project_id=wr.project_id
			  AND active.issue_id=wr.issue_id
			  AND active.agent_id=wr.target_agent_id
			  AND active.status = ANY($1::text[])
		  )
		ORDER BY wr.updated_at, wr.created_at, wr.id
		LIMIT 1
	`, activeRunStatuses))
	if errors.Is(err, store.ErrNotFound) {
		return store.AgentWorkRequestReconciliationResult{}, nil
	}
	if err != nil {
		return store.AgentWorkRequestReconciliationResult{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.AgentWorkRequestReconciliationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	issue, _, _, err := lockAssignmentIssue(ctx, tx, candidate.ProjectID, candidate.IssueID)
	if err != nil {
		return store.AgentWorkRequestReconciliationResult{}, err
	}
	current, err := scanAgentWorkRequest(tx.QueryRow(ctx, `
		SELECT `+agentWorkRequestSelectColumns+`
		FROM agent_work_requests
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, candidate.ProjectID, candidate.ID))
	if err != nil {
		return store.AgentWorkRequestReconciliationResult{}, err
	}
	if current.AuthorityKind != store.AgentWorkRequestAuthorityIssue || current.RunID != nil || current.DelegationID != nil || current.SealedAt != nil || current.ClosedAt != nil {
		if err := tx.Commit(ctx); err != nil {
			return store.AgentWorkRequestReconciliationResult{}, err
		}
		return store.AgentWorkRequestReconciliationResult{Handled: true}, nil
	}
	if current.IssueID != issue.ID {
		return store.AgentWorkRequestReconciliationResult{}, store.ErrConflict
	}
	if _, err := lockActiveRunForAgentWork(ctx, tx, current.ProjectID, current.IssueID, current.TargetAgentID); err == nil {
		if err := touchAgentWorkRequestTx(ctx, tx, current.ID); err != nil {
			return store.AgentWorkRequestReconciliationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return store.AgentWorkRequestReconciliationResult{}, err
		}
		return store.AgentWorkRequestReconciliationResult{}, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.AgentWorkRequestReconciliationResult{}, err
	}
	if err := s.verifyRunnableAgent(ctx, tx, current.ProjectID, current.TargetAgentID); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
			if err := touchAgentWorkRequestTx(ctx, tx, current.ID); err != nil {
				return store.AgentWorkRequestReconciliationResult{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return store.AgentWorkRequestReconciliationResult{}, err
			}
			return store.AgentWorkRequestReconciliationResult{}, nil
		}
		return store.AgentWorkRequestReconciliationResult{}, err
	}

	commentID, task, found, err := agentWorkRequestAnchorTx(ctx, tx, current.ProjectID, current.IssueID, current.ID)
	if err != nil {
		return store.AgentWorkRequestReconciliationResult{}, err
	}
	if !found || strings.TrimSpace(task) == "" || utf8.RuneCountInString(strings.TrimSpace(task)) > store.MaxDelegationTaskCharacters {
		if err := closeAgentWorkRequestTx(ctx, tx, current.ID); err != nil {
			return store.AgentWorkRequestReconciliationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return store.AgentWorkRequestReconciliationResult{}, err
		}
		return store.AgentWorkRequestReconciliationResult{Handled: true}, nil
	}
	delegation, err := s.requestIssueDelegationTx(ctx, tx, store.RequestIssueDelegationCommand{
		ProjectID: current.ProjectID, IssueID: current.IssueID, SourceCommentID: commentID,
		TargetAgentID: current.TargetAgentID, Task: strings.TrimSpace(task), RequestKey: agentWorkRequestDelegationKey(current.ID),
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			if err := touchAgentWorkRequestTx(ctx, tx, current.ID); err != nil {
				return store.AgentWorkRequestReconciliationResult{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return store.AgentWorkRequestReconciliationResult{}, err
			}
			return store.AgentWorkRequestReconciliationResult{}, nil
		}
		return store.AgentWorkRequestReconciliationResult{}, err
	}
	updated, err := scanAgentWorkRequest(tx.QueryRow(ctx, `
		UPDATE agent_work_requests
		SET run_id=$3, delegation_id=$4, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND run_id IS NULL AND delegation_id IS NULL AND sealed_at IS NULL AND closed_at IS NULL
		RETURNING `+agentWorkRequestSelectColumns,
		current.ProjectID, current.ID, delegation.DelegatedRun.ID, delegation.Delegation.ID,
	))
	if err != nil {
		return store.AgentWorkRequestReconciliationResult{}, err
	}
	if updated.RunID == nil || *updated.RunID != delegation.DelegatedRun.ID {
		return store.AgentWorkRequestReconciliationResult{}, store.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return store.AgentWorkRequestReconciliationResult{}, err
	}
	return store.AgentWorkRequestReconciliationResult{Handled: true, Events: delegation.Events}, nil
}

func agentWorkRequestAnchorTx(ctx context.Context, tx pgx.Tx, projectID, issueID, requestID string) (string, string, bool, error) {
	var commentID, body string
	err := tx.QueryRow(ctx, `
		WITH linked AS (
			SELECT comment_id FROM issue_comment_mentions
			WHERE project_id=$1 AND issue_id=$2 AND work_request_id=$3
			UNION
			SELECT comment_id FROM issue_comment_implicit_triggers
			WHERE project_id=$1 AND issue_id=$2 AND work_request_id=$3
		)
		SELECT c.id::text, COALESCE(c.body, '')
		FROM linked
		JOIN issue_comments AS c ON c.issue_id=$2 AND c.id=linked.comment_id
		WHERE c.author_type='HUMAN' AND c.deleted_at IS NULL
		ORDER BY c.created_at, c.id
		LIMIT 1
	`, projectID, issueID, requestID).Scan(&commentID, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return commentID, body, true, nil
}

func sealAgentWorkRequestsForRunTx(ctx context.Context, tx pgx.Tx, projectID, runID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE agent_work_requests
		SET sealed_at=COALESCE(sealed_at, now()), updated_at=now()
		WHERE project_id=$1 AND run_id=$2 AND sealed_at IS NULL
	`, projectID, runID)
	return err
}

func closeAgentWorkRequestsForRunTx(ctx context.Context, tx pgx.Tx, projectID, runID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE agent_work_requests
		SET sealed_at=COALESCE(sealed_at, now()), closed_at=COALESCE(closed_at, now()), updated_at=now()
		WHERE project_id=$1 AND run_id=$2 AND closed_at IS NULL
	`, projectID, runID)
	return err
}

func closeAgentWorkRequestTx(ctx context.Context, tx pgx.Tx, requestID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE agent_work_requests
		SET sealed_at=COALESCE(sealed_at, now()), closed_at=COALESCE(closed_at, now()), updated_at=now()
		WHERE id=$1 AND closed_at IS NULL
	`, requestID)
	return err
}

func touchAgentWorkRequestTx(ctx context.Context, tx pgx.Tx, requestID string) error {
	_, err := tx.Exec(ctx, `UPDATE agent_work_requests SET updated_at=now() WHERE id=$1`, requestID)
	return err
}

func scanAgentWorkRequest(row pgx.Row) (store.AgentWorkRequest, error) {
	var value store.AgentWorkRequest
	if err := row.Scan(
		&value.ID, &value.ProjectID, &value.IssueID, &value.WorkspaceID, &value.TargetAgentID,
		&value.AuthorityKind, &value.ParentRunID, &value.RunID, &value.DelegationID,
		&value.SealedAt, &value.ClosedAt, &value.CreatedAt, &value.UpdatedAt,
	); err != nil {
		return store.AgentWorkRequest{}, notFound(err)
	}
	return value, nil
}

func agentWorkRequestDelegationKey(requestID string) string {
	return fmt.Sprintf("agent-work-request:%s", requestID)
}
