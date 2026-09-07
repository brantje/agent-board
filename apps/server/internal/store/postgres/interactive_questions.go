package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) OpenInteractiveQuestion(ctx context.Context, input store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionResult, error) {
	questionInput := input.Question
	if strings.TrimSpace(questionInput.ProjectID) == "" || strings.TrimSpace(questionInput.IssueID) == "" ||
		strings.TrimSpace(questionInput.RunID) == "" || strings.TrimSpace(questionInput.Prompt) == "" ||
		strings.TrimSpace(input.Engine) == "" || strings.TrimSpace(input.CorrelationKey) == "" || !questionInput.Blocking {
		return store.OpenInteractiveQuestionResult{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, questionInput.ProjectID, questionInput.RunID))
	if err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}
	if run.IssueID != questionInput.IssueID {
		return store.OpenInteractiveQuestionResult{}, store.ErrConflict
	}

	binding, err := scanInteractiveQuestionBinding(tx.QueryRow(ctx, `
		SELECT question_id::text, project_id::text, run_id::text, engine, correlation_key, state, created_at, updated_at
		FROM engine_question_bindings
		WHERE project_id=$1 AND run_id=$2 AND engine=$3 AND correlation_key=$4
	`, questionInput.ProjectID, questionInput.RunID, input.Engine, input.CorrelationKey))
	if err == nil {
		question, getErr := scanQuestion(tx.QueryRow(ctx, `
			SELECT id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, blocking, status, created_at, answered_at
			FROM questions
			WHERE project_id=$1 AND id=$2
		`, binding.ProjectID, binding.QuestionID))
		if getErr != nil {
			return store.OpenInteractiveQuestionResult{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return store.OpenInteractiveQuestionResult{}, err
		}
		return store.OpenInteractiveQuestionResult{Question: question, Binding: binding, Run: run}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.OpenInteractiveQuestionResult{}, err
	}

	if run.Status != "RUNNING" && run.Status != "WAITING_FOR_INPUT" {
		return store.OpenInteractiveQuestionResult{}, store.ErrConflict
	}
	live, err := hasLiveClaim(ctx, tx, questionInput.ProjectID, questionInput.RunID)
	if err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}
	if !live {
		return store.OpenInteractiveQuestionResult{}, store.ErrConflict
	}

	kind := questionInput.Kind
	if kind == "" {
		kind = "TEXT"
	}
	status := questionInput.Status
	if status == "" {
		status = "OPEN"
	}
	question, err := scanQuestion(tx.QueryRow(ctx, `
		INSERT INTO questions (project_id, issue_id, run_id, prompt, kind, options, recommendation, blocking, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true, $8)
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, blocking, status, created_at, answered_at
	`, questionInput.ProjectID, questionInput.IssueID, questionInput.RunID, questionInput.Prompt, kind, arrayJSON(questionInput.Options), questionInput.Recommendation, status))
	if err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}

	binding, err = scanInteractiveQuestionBinding(tx.QueryRow(ctx, `
		INSERT INTO engine_question_bindings (question_id, project_id, run_id, engine, correlation_key, state)
		VALUES ($1, $2, $3, $4, $5, 'OPEN')
		RETURNING question_id::text, project_id::text, run_id::text, engine, correlation_key, state, created_at, updated_at
	`, question.ID, question.ProjectID, question.RunID, input.Engine, input.CorrelationKey))
	if err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}

	enteredWaiting := run.Status == "RUNNING"
	if enteredWaiting {
		run, err = scanRun(tx.QueryRow(ctx, `
			UPDATE runs
			SET status='WAITING_FOR_INPUT', queue_reason=NULL, updated_at=now()
			WHERE project_id=$1 AND id=$2 AND status='RUNNING'
			RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		`, question.ProjectID, question.RunID))
		if err != nil {
			return store.OpenInteractiveQuestionResult{}, err
		}
		command, err := tx.Exec(ctx, `
			UPDATE issues
			SET status=CASE WHEN status='DONE' THEN status ELSE 'BLOCKED' END,
			    updated_at=CASE WHEN status='DONE' THEN updated_at ELSE now() END
			WHERE project_id=$1 AND id=$2
		`, question.ProjectID, question.IssueID)
		if err != nil {
			return store.OpenInteractiveQuestionResult{}, err
		}
		if command.RowsAffected() != 1 {
			return store.OpenInteractiveQuestionResult{}, store.ErrConflict
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}
	return store.OpenInteractiveQuestionResult{
		Question:       question,
		Binding:        binding,
		Run:            run,
		Created:        true,
		EnteredWaiting: enteredWaiting,
	}, nil
}

func (s *Store) ResolveInteractiveQuestion(ctx context.Context, projectID, questionID string) (store.ResolveInteractiveQuestionResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(questionID) == "" {
		return store.ResolveInteractiveQuestionResult{}, store.ErrInvalidArgument
	}

	var runID string
	if err := s.pool.QueryRow(ctx, `
		SELECT run_id::text
		FROM engine_question_bindings
		WHERE project_id=$1 AND question_id=$2
	`, projectID, questionID).Scan(&runID); err != nil {
		return store.ResolveInteractiveQuestionResult{}, notFound(err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, projectID, runID))
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	binding, err := scanInteractiveQuestionBinding(tx.QueryRow(ctx, `
		SELECT question_id::text, project_id::text, run_id::text, engine, correlation_key, state, created_at, updated_at
		FROM engine_question_bindings
		WHERE project_id=$1 AND question_id=$2
		FOR UPDATE
	`, projectID, questionID))
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	if binding.RunID != run.ID {
		return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
	}
	if binding.State == store.InteractiveQuestionResolved {
		if err := tx.Commit(ctx); err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		return store.ResolveInteractiveQuestionResult{Binding: binding, Run: run}, nil
	}
	if binding.State != store.InteractiveQuestionAnswered {
		return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
	}

	binding, err = scanInteractiveQuestionBinding(tx.QueryRow(ctx, `
		UPDATE engine_question_bindings
		SET state='RESOLVED', updated_at=now()
		WHERE project_id=$1 AND question_id=$2 AND state='ANSWERED'
		RETURNING question_id::text, project_id::text, run_id::text, engine, correlation_key, state, created_at, updated_at
	`, projectID, questionID))
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}

	var unresolved int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM engine_question_bindings
		WHERE project_id=$1 AND run_id=$2 AND state IN ('OPEN', 'ANSWERED')
	`, projectID, run.ID).Scan(&unresolved); err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}

	resumed := false
	if unresolved == 0 {
		if run.Status != "WAITING_FOR_INPUT" {
			return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
		}
		live, err := hasLiveClaim(ctx, tx, projectID, run.ID)
		if err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		if !live {
			return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
		}
		run, err = scanRun(tx.QueryRow(ctx, `
			UPDATE runs
			SET status='RUNNING', queue_reason=NULL, updated_at=now()
			WHERE project_id=$1 AND id=$2 AND status='WAITING_FOR_INPUT'
			RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		`, projectID, run.ID))
		if err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		command, err := tx.Exec(ctx, `
			UPDATE issues
			SET status=CASE WHEN status='DONE' THEN status ELSE 'IN_PROGRESS' END,
			    updated_at=CASE WHEN status='DONE' THEN updated_at ELSE now() END
			WHERE project_id=$1 AND id=$2
		`, projectID, run.IssueID)
		if err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		if command.RowsAffected() != 1 {
			return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
		}
		resumed = true
	}

	if err := tx.Commit(ctx); err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	return store.ResolveInteractiveQuestionResult{Binding: binding, Run: run, Resumed: resumed}, nil
}

func hasLiveClaim(ctx context.Context, tx pgx.Tx, projectID, runID string) (bool, error) {
	var live bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM scheduler_jobs AS job
			JOIN scheduler_leases AS lease ON lease.job_id=job.id
			WHERE job.project_id=$1 AND job.run_id=$2 AND job.state='CLAIMED' AND lease.expires_at > now()
		)
	`, projectID, runID).Scan(&live)
	return live, err
}

func scanInteractiveQuestionBinding(row pgx.Row) (store.InteractiveQuestionBinding, error) {
	var value store.InteractiveQuestionBinding
	if err := row.Scan(&value.QuestionID, &value.ProjectID, &value.RunID, &value.Engine, &value.CorrelationKey, &value.State, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.InteractiveQuestionBinding{}, notFound(err)
	}
	return value, nil
}

var _ store.InteractiveQuestionStore = (*Store)(nil)
