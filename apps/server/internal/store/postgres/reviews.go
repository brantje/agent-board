package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const maxReviewFeedbackCharacters = 32 << 10

func (s *Store) GetReview(ctx context.Context, projectID, reviewID string) (store.Review, error) {
	return scanReview(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text,
		       requested_at, decided_at, created_at, updated_at
		FROM reviews WHERE project_id=$1 AND id=$2
	`, projectID, reviewID))
}

func (s *Store) ListReviews(ctx context.Context, projectID string, filter store.ReviewFilter) ([]store.Review, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text,
		       requested_at, decided_at, created_at, updated_at
		FROM reviews
		WHERE project_id=$1
		  AND ($2::uuid IS NULL OR issue_id=$2)
		  AND (coalesce(cardinality($3::text[]), 0)=0 OR status=ANY($3::text[]))
		ORDER BY requested_at, id
	`, projectID, filter.IssueID, filter.Statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]store.Review, 0)
	for rows.Next() {
		value, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) GetDecision(ctx context.Context, projectID, decisionID string) (store.Decision, error) {
	return scanDecision(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, question_id::text,
		       kind, outcome, actor_type, actor_id, safe_details, created_at
		FROM decisions WHERE project_id=$1 AND id=$2
	`, projectID, decisionID))
}

func (s *Store) BeginReviewApproval(ctx context.Context, input store.BeginReviewApprovalCommand) (store.BeginReviewApprovalResult, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.ReviewID) == "" {
		return store.BeginReviewApprovalResult{}, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.BeginReviewApprovalResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	review, run, issue, err := lockReviewCommandState(ctx, tx, input.ProjectID, input.ReviewID)
	if err != nil {
		return store.BeginReviewApprovalResult{}, err
	}
	if review.Status != "PENDING" || run.Status != "READY_FOR_REVIEW" || issue.Status != "REVIEW" {
		return store.BeginReviewApprovalResult{}, store.ErrConflict
	}
	if err := ensureLatestReviewAttempt(ctx, tx, run); err != nil {
		return store.BeginReviewApprovalResult{}, err
	}

	if review.DecisionID != nil {
		decision, err := decisionByIDTx(ctx, tx, review.ProjectID, *review.DecisionID)
		if err != nil {
			return store.BeginReviewApprovalResult{}, err
		}
		if decision.Kind == "REVIEW_APPROVAL_INTENT" && decision.Outcome == "PENDING" {
			if err := tx.Commit(ctx); err != nil {
				return store.BeginReviewApprovalResult{}, err
			}
			return store.BeginReviewApprovalResult{Review: review, Decision: decision, Run: run}, nil
		}
		if decision.Kind != "REVIEW_APPROVAL_FAILED" {
			return store.BeginReviewApprovalResult{}, store.ErrConflict
		}
	}

	decision, err := scanDecision(tx.QueryRow(ctx, `
		INSERT INTO decisions (project_id, issue_id, run_id, kind, outcome, actor_type, actor_id, safe_details)
		VALUES ($1, $2, $3, 'REVIEW_APPROVAL_INTENT', 'PENDING', 'HUMAN', $4, '{}'::jsonb)
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, question_id::text,
		          kind, outcome, actor_type, actor_id, safe_details, created_at
	`, review.ProjectID, review.IssueID, review.RunID, input.ActorID))
	if err != nil {
		return store.BeginReviewApprovalResult{}, err
	}
	review, err = scanReview(tx.QueryRow(ctx, `
		UPDATE reviews SET decision_id=$3, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='PENDING'
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text,
		          requested_at, decided_at, created_at, updated_at
	`, review.ProjectID, review.ID, decision.ID))
	if err != nil {
		return store.BeginReviewApprovalResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.BeginReviewApprovalResult{}, err
	}
	return store.BeginReviewApprovalResult{Review: review, Decision: decision, Run: run}, nil
}

func (s *Store) CompleteReviewApproval(ctx context.Context, input store.CompleteReviewApprovalCommand) (store.CompleteReviewApprovalResult, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.ReviewID) == "" || strings.TrimSpace(input.AcceptedRevision) == "" {
		return store.CompleteReviewApprovalResult{}, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	review, run, issue, err := lockReviewCommandState(ctx, tx, input.ProjectID, input.ReviewID)
	if err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	if review.Status == "APPROVED" {
		if review.DecisionID == nil {
			return store.CompleteReviewApprovalResult{}, store.ErrConflict
		}
		if input.DeliveryComplete {
			if run.Status != "COMPLETED" || issue.Status != "DONE" {
				return store.CompleteReviewApprovalResult{}, store.ErrConflict
			}
		} else if run.Status != "READY_FOR_REVIEW" || issue.Status != "REVIEW" {
			return store.CompleteReviewApprovalResult{}, store.ErrConflict
		}
		decision, err := decisionByIDTx(ctx, tx, review.ProjectID, *review.DecisionID)
		if err != nil {
			return store.CompleteReviewApprovalResult{}, err
		}
		if decision.Kind != "REVIEW" || decision.Outcome != "APPROVED" || !decisionHasAcceptedRevision(decision, input.AcceptedRevision) {
			return store.CompleteReviewApprovalResult{}, store.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return store.CompleteReviewApprovalResult{}, err
		}
		return store.CompleteReviewApprovalResult{Review: review, Decision: decision, Run: run, Issue: issue}, nil
	}
	if review.Status != "PENDING" || run.Status != "READY_FOR_REVIEW" || issue.Status != "REVIEW" || review.DecisionID == nil {
		return store.CompleteReviewApprovalResult{}, store.ErrConflict
	}
	if err := ensureLatestReviewAttempt(ctx, tx, run); err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	intent, err := decisionByIDTx(ctx, tx, review.ProjectID, *review.DecisionID)
	if err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	if intent.Kind != "REVIEW_APPROVAL_INTENT" || intent.Outcome != "PENDING" {
		return store.CompleteReviewApprovalResult{}, store.ErrConflict
	}

	details, err := json.Marshal(map[string]any{
		"acceptedRevision": input.AcceptedRevision,
		"deliveryComplete": input.DeliveryComplete,
	})
	if err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	decision, err := scanDecision(tx.QueryRow(ctx, `
		INSERT INTO decisions (project_id, issue_id, run_id, kind, outcome, actor_type, actor_id, safe_details)
		VALUES ($1, $2, $3, 'REVIEW', 'APPROVED', $4, $5, $6)
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, question_id::text,
		          kind, outcome, actor_type, actor_id, safe_details, created_at
	`, review.ProjectID, review.IssueID, review.RunID, intent.ActorType, intent.ActorID, details))
	if err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	review, err = scanReview(tx.QueryRow(ctx, `
		UPDATE reviews
		SET status='APPROVED', decision_id=$3, decided_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='PENDING'
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text,
		          requested_at, decided_at, created_at, updated_at
	`, review.ProjectID, review.ID, decision.ID))
	if err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	if input.DeliveryComplete {
		run, err = scanRun(tx.QueryRow(ctx, `
			UPDATE runs SET status='COMPLETED', completed_at=now(), updated_at=now()
			WHERE project_id=$1 AND id=$2 AND status='READY_FOR_REVIEW'
			RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
			          status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		`, run.ProjectID, run.ID))
		if err != nil {
			return store.CompleteReviewApprovalResult{}, err
		}
		issue, err = scanIssueJoined(tx.QueryRow(ctx, `
			UPDATE issues AS i SET status='DONE', updated_at=now()
			FROM projects AS p
			WHERE i.project_id=$1 AND i.id=$2 AND i.status='REVIEW' AND p.id=i.project_id
			RETURNING `+issueSelectColumns+`
		`, issue.ProjectID, issue.ID))
		if err != nil {
			return store.CompleteReviewApprovalResult{}, err
		}
	}
	events, err := appendReviewDecisionEvents(ctx, tx, run, review, decision, "review.approved")
	if err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	return store.CompleteReviewApprovalResult{Review: review, Decision: decision, Run: run, Issue: issue, Events: events}, nil
}

func (s *Store) FailReviewApproval(ctx context.Context, input store.FailReviewApprovalCommand) (store.Review, error) {
	reason := strings.TrimSpace(input.Reason)
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.ReviewID) == "" || reason == "" || len(reason) > 4096 {
		return store.Review{}, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.Review{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	review, run, issue, err := lockReviewCommandState(ctx, tx, input.ProjectID, input.ReviewID)
	if err != nil {
		return store.Review{}, err
	}
	if review.Status != "PENDING" || run.Status != "READY_FOR_REVIEW" || issue.Status != "REVIEW" || review.DecisionID == nil {
		return store.Review{}, store.ErrConflict
	}
	intent, err := decisionByIDTx(ctx, tx, review.ProjectID, *review.DecisionID)
	if err != nil {
		return store.Review{}, err
	}
	if intent.Kind != "REVIEW_APPROVAL_INTENT" || intent.Outcome != "PENDING" {
		return store.Review{}, store.ErrConflict
	}
	details, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		return store.Review{}, err
	}
	failed, err := scanDecision(tx.QueryRow(ctx, `
		INSERT INTO decisions (project_id, issue_id, run_id, kind, outcome, actor_type, actor_id, safe_details)
		VALUES ($1, $2, $3, 'REVIEW_APPROVAL_FAILED', 'FAILED', $4, $5, $6)
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, question_id::text,
		          kind, outcome, actor_type, actor_id, safe_details, created_at
	`, review.ProjectID, review.IssueID, review.RunID, intent.ActorType, intent.ActorID, details))
	if err != nil {
		return store.Review{}, err
	}
	review, err = scanReview(tx.QueryRow(ctx, `
		UPDATE reviews SET decision_id=$3, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='PENDING'
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text,
		          requested_at, decided_at, created_at, updated_at
	`, review.ProjectID, review.ID, failed.ID))
	if err != nil {
		return store.Review{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Review{}, err
	}
	return review, nil
}

func (s *Store) RequestReviewChanges(ctx context.Context, input store.RequestReviewChangesCommand) (store.RequestReviewChangesResult, error) {
	feedback := strings.TrimSpace(input.Feedback)
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.ReviewID) == "" || feedback == "" || utf8.RuneCountInString(feedback) > maxReviewFeedbackCharacters {
		return store.RequestReviewChangesResult{}, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	review, run, issue, err := lockReviewCommandState(ctx, tx, input.ProjectID, input.ReviewID)
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	if review.Status == "CHANGES_REQUESTED" {
		result, err := existingRequestedChanges(ctx, tx, review, issue)
		if err != nil {
			return store.RequestReviewChangesResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return store.RequestReviewChangesResult{}, err
		}
		return result, nil
	}
	if review.Status != "PENDING" || run.Status != "READY_FOR_REVIEW" || issue.Status != "REVIEW" || run.AgentID == nil {
		return store.RequestReviewChangesResult{}, store.ErrConflict
	}
	if err := ensureLatestReviewAttempt(ctx, tx, run); err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	if review.DecisionID != nil {
		decision, err := decisionByIDTx(ctx, tx, review.ProjectID, *review.DecisionID)
		if err != nil {
			return store.RequestReviewChangesResult{}, err
		}
		if decision.Kind == "REVIEW_APPROVAL_INTENT" && decision.Outcome == "PENDING" {
			return store.RequestReviewChangesResult{}, store.ErrConflict
		}
	}

	details, err := json.Marshal(map[string]string{"feedback": feedback})
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	decision, err := scanDecision(tx.QueryRow(ctx, `
		INSERT INTO decisions (project_id, issue_id, run_id, kind, outcome, actor_type, actor_id, safe_details)
		VALUES ($1, $2, $3, 'REVIEW', 'CHANGES_REQUESTED', 'HUMAN', $4, $5)
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, question_id::text,
		          kind, outcome, actor_type, actor_id, safe_details, created_at
	`, review.ProjectID, review.IssueID, review.RunID, input.ActorID, details))
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	review, err = scanReview(tx.QueryRow(ctx, `
		UPDATE reviews
		SET status='CHANGES_REQUESTED', decision_id=$3, decided_at=now(), updated_at=now()
		WHERE project_id=$1 AND id=$2 AND status='PENDING'
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text,
		          requested_at, decided_at, created_at, updated_at
	`, review.ProjectID, review.ID, decision.ID))
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}

	nextRun, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO runs (project_id, issue_id, workspace_id, agent_id, attempt, status)
		VALUES ($1, $2, $3, $4, $5, 'QUEUED')
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		          status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, run.ProjectID, run.IssueID, run.WorkspaceID, run.AgentID, run.Attempt+1))
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	job, err := scanSchedulerJob(tx.QueryRow(ctx, `
		INSERT INTO scheduler_jobs (project_id, run_id, kind, state, idempotency_key, available_at)
		VALUES ($1, $2, 'START', 'QUEUED', $3, now())
		RETURNING id::text, project_id::text, run_id::text, kind, state, wait_reason,
		          idempotency_key, available_at, created_at, updated_at
	`, review.ProjectID, nextRun.ID, "review:"+review.ID+":changes"))
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	issue, err = scanIssueJoined(tx.QueryRow(ctx, `
		UPDATE issues AS i SET status='IN_PROGRESS', updated_at=now()
		FROM projects AS p
		WHERE i.project_id=$1 AND i.id=$2 AND i.status='REVIEW' AND p.id=i.project_id
		RETURNING `+issueSelectColumns+`
	`, issue.ProjectID, issue.ID))
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	events, err := appendReviewDecisionEvents(ctx, tx, run, review, decision, "review.changes_requested")
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	return store.RequestReviewChangesResult{Review: review, Decision: decision, Run: nextRun, Job: job, Issue: issue, Events: events}, nil
}

func lockReviewCommandState(ctx context.Context, tx pgx.Tx, projectID, reviewID string) (store.Review, store.Run, store.Issue, error) {
	initial, err := scanReview(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text,
		       requested_at, decided_at, created_at, updated_at
		FROM reviews WHERE project_id=$1 AND id=$2
	`, projectID, reviewID))
	if err != nil {
		return store.Review{}, store.Run{}, store.Issue{}, err
	}
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs WHERE project_id=$1 AND id=$2 AND issue_id=$3 FOR UPDATE
	`, initial.ProjectID, initial.RunID, initial.IssueID))
	if err != nil {
		return store.Review{}, store.Run{}, store.Issue{}, err
	}
	review, err := scanReview(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text,
		       requested_at, decided_at, created_at, updated_at
		FROM reviews WHERE project_id=$1 AND id=$2 FOR UPDATE
	`, projectID, reviewID))
	if err != nil {
		return store.Review{}, store.Run{}, store.Issue{}, err
	}
	if review.RunID != initial.RunID || review.IssueID != initial.IssueID {
		return store.Review{}, store.Run{}, store.Issue{}, store.ErrConflict
	}
	issue, err := scanIssueJoined(tx.QueryRow(ctx, `
		SELECT `+issueSelectColumns+`
		FROM issues AS i
		JOIN projects AS p ON p.id=i.project_id
		WHERE i.project_id=$1 AND i.id=$2
		FOR UPDATE OF i
	`, review.ProjectID, review.IssueID))
	if err != nil {
		return store.Review{}, store.Run{}, store.Issue{}, err
	}
	return review, run, issue, nil
}

func ensureLatestReviewAttempt(ctx context.Context, tx pgx.Tx, run store.Run) error {
	var newer bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE project_id=$1 AND issue_id=$2 AND attempt>$3)`, run.ProjectID, run.IssueID, run.Attempt).Scan(&newer); err != nil {
		return err
	}
	if newer {
		return store.ErrConflict
	}
	return nil
}

func decisionByIDTx(ctx context.Context, tx pgx.Tx, projectID, decisionID string) (store.Decision, error) {
	return scanDecision(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, question_id::text,
		       kind, outcome, actor_type, actor_id, safe_details, created_at
		FROM decisions WHERE project_id=$1 AND id=$2
	`, projectID, decisionID))
}

func decisionHasAcceptedRevision(decision store.Decision, revision string) bool {
	var details struct {
		AcceptedRevision string `json:"acceptedRevision"`
	}
	return json.Unmarshal(decision.SafeDetails, &details) == nil && details.AcceptedRevision == revision
}

func existingRequestedChanges(ctx context.Context, tx pgx.Tx, review store.Review, issue store.Issue) (store.RequestReviewChangesResult, error) {
	if review.DecisionID == nil {
		return store.RequestReviewChangesResult{}, store.ErrConflict
	}
	decision, err := decisionByIDTx(ctx, tx, review.ProjectID, *review.DecisionID)
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	if decision.Kind != "REVIEW" || decision.Outcome != "CHANGES_REQUESTED" {
		return store.RequestReviewChangesResult{}, store.ErrConflict
	}
	job, err := scanSchedulerJob(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, run_id::text, kind, state, wait_reason,
		       idempotency_key, available_at, created_at, updated_at
		FROM scheduler_jobs WHERE project_id=$1 AND idempotency_key=$2
	`, review.ProjectID, "review:"+review.ID+":changes"))
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt,
		       status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs WHERE project_id=$1 AND id=$2
	`, review.ProjectID, job.RunID))
	if err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	return store.RequestReviewChangesResult{Review: review, Decision: decision, Run: run, Job: job, Issue: issue}, nil
}

func appendReviewDecisionEvents(ctx context.Context, tx pgx.Tx, run store.Run, review store.Review, decision store.Decision, reviewEventType string) ([]store.Event, error) {
	reviewPayload, err := json.Marshal(map[string]any{
		"reviewId":   review.ID,
		"decisionId": decision.ID,
	})
	if err != nil {
		return nil, err
	}
	decisionPayload, err := json.Marshal(map[string]any{
		"decisionId": decision.ID,
		"reviewId":   review.ID,
		"kind":       decision.Kind,
		"outcome":    decision.Outcome,
		"actorType":  decision.ActorType,
		"actorId":    decision.ActorID,
	})
	if err != nil {
		return nil, err
	}
	issueID, runID, workspaceID := run.IssueID, run.ID, run.WorkspaceID
	reviewEvent, err := appendEventTx(ctx, tx, store.Event{
		Type:        reviewEventType,
		ProjectID:   review.ProjectID,
		IssueID:     &issueID,
		RunID:       &runID,
		AgentID:     run.AgentID,
		WorkspaceID: &workspaceID,
		Actor:       store.EmptyObject,
		Payload:     reviewPayload,
	})
	if err != nil {
		return nil, err
	}
	decisionEvent, err := appendEventTx(ctx, tx, store.Event{
		Type:        "decision.recorded",
		ProjectID:   review.ProjectID,
		IssueID:     &issueID,
		RunID:       &runID,
		AgentID:     run.AgentID,
		WorkspaceID: &workspaceID,
		Actor:       store.EmptyObject,
		Payload:     decisionPayload,
	})
	if err != nil {
		return nil, err
	}
	return []store.Event{reviewEvent, decisionEvent}, nil
}
