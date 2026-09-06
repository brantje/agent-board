package postgres

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetQuestion(ctx context.Context, projectID, questionID string) (store.Question, error) {
	return scanQuestion(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, blocking, status, created_at, answered_at
		FROM questions
		WHERE project_id = $1 AND id = $2
	`, projectID, questionID))
}

func (s *Store) ListQuestions(ctx context.Context, projectID string, filter store.QuestionFilter) ([]store.Question, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, blocking, status, created_at, answered_at
		FROM questions
		WHERE project_id = $1
		  AND ($2::uuid IS NULL OR issue_id = $2)
		  AND ($3::uuid IS NULL OR run_id = $3)
		  AND (cardinality($4::text[]) = 0 OR status = ANY($4::text[]))
		ORDER BY created_at, id
	`, projectID, filter.IssueID, filter.RunID, filter.Statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	questions := make([]store.Question, 0)
	for rows.Next() {
		question, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return questions, nil
}

func (s *Store) GetDecisionByQuestion(ctx context.Context, projectID, questionID string) (store.Decision, error) {
	return scanDecision(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, question_id::text, kind, outcome, actor_type, actor_id, safe_details, created_at
		FROM decisions
		WHERE project_id = $1 AND question_id = $2
		ORDER BY created_at, id
		LIMIT 1
	`, projectID, questionID))
}

func (s *Store) GetOpenBlockingQuestion(ctx context.Context, projectID, runID string) (store.Question, error) {
	return scanQuestion(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, blocking, status, created_at, answered_at
		FROM questions
		WHERE project_id = $1 AND run_id = $2 AND blocking AND status = 'OPEN'
		ORDER BY created_at, id
		LIMIT 1
	`, projectID, runID))
}

func (s *Store) AnswerQuestion(ctx context.Context, input store.AnswerQuestionCommand) (store.AnswerQuestionResult, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.QuestionID) == "" || input.ActorType != "HUMAN" {
		return store.AnswerQuestionResult{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.AnswerQuestionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	question, err := scanQuestion(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, blocking, status, created_at, answered_at
		FROM questions
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, input.ProjectID, input.QuestionID))
	if err != nil {
		return store.AnswerQuestionResult{}, err
	}
	if question.Status != "OPEN" {
		return store.AnswerQuestionResult{}, store.ErrConflict
	}
	if err := validateQuestionAnswer(question, input.Answer); err != nil {
		return store.AnswerQuestionResult{}, err
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id = $1 AND id = $2 AND issue_id = $3
		FOR UPDATE
	`, question.ProjectID, question.RunID, question.IssueID))
	if err != nil {
		return store.AnswerQuestionResult{}, err
	}

	var issueStatus string
	if err := tx.QueryRow(ctx, `
		SELECT status
		FROM issues
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, question.ProjectID, question.IssueID).Scan(&issueStatus); err != nil {
		return store.AnswerQuestionResult{}, notFound(err)
	}

	if question.Blocking {
		if run.Status != "WAITING_FOR_INPUT" || issueStatus != "BLOCKED" {
			return store.AnswerQuestionResult{}, store.ErrConflict
		}
		var activeJob bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM scheduler_jobs
				WHERE project_id = $1 AND run_id = $2 AND state IN ('QUEUED', 'CLAIMED')
			)
		`, question.ProjectID, question.RunID).Scan(&activeJob); err != nil {
			return store.AnswerQuestionResult{}, err
		}
		if activeJob {
			return store.AnswerQuestionResult{}, store.ErrConflict
		}
	}

	question, err = scanQuestion(tx.QueryRow(ctx, `
		UPDATE questions
		SET status = 'ANSWERED', answered_at = now()
		WHERE project_id = $1 AND id = $2 AND status = 'OPEN'
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, blocking, status, created_at, answered_at
	`, question.ProjectID, question.ID))
	if err != nil {
		return store.AnswerQuestionResult{}, err
	}

	details, err := json.Marshal(map[string]any{"questionAnswer": input.Answer})
	if err != nil {
		return store.AnswerQuestionResult{}, err
	}
	questionID, issueID, runID := question.ID, question.IssueID, question.RunID
	decision, err := scanDecision(tx.QueryRow(ctx, `
		INSERT INTO decisions (project_id, issue_id, run_id, question_id, kind, outcome, actor_type, actor_id, safe_details)
		VALUES ($1, $2, $3, $4, 'QUESTION_ANSWER', $5, $6, $7, $8)
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, question_id::text, kind, outcome, actor_type, actor_id, safe_details, created_at
	`, question.ProjectID, issueID, runID, questionID, questionAnswerOutcome(input.Answer), input.ActorType, input.ActorID, details))
	if err != nil {
		return store.AnswerQuestionResult{}, err
	}

	result := store.AnswerQuestionResult{Question: question, Decision: decision, Run: run}
	if question.Blocking {
		run, err = scanRun(tx.QueryRow(ctx, `
			UPDATE runs
			SET status = 'QUEUED', queue_reason = NULL, failure_reason = NULL, completed_at = NULL, updated_at = now()
			WHERE project_id = $1 AND id = $2 AND status = 'WAITING_FOR_INPUT'
			RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		`, question.ProjectID, question.RunID))
		if err != nil {
			return store.AnswerQuestionResult{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE issues
			SET status = 'IN_PROGRESS', updated_at = now()
			WHERE project_id = $1 AND id = $2 AND status = 'BLOCKED'
		`, question.ProjectID, question.IssueID); err != nil {
			return store.AnswerQuestionResult{}, err
		}
		job, err := scanSchedulerJob(tx.QueryRow(ctx, `
			INSERT INTO scheduler_jobs (project_id, run_id, kind, state, idempotency_key, available_at)
			VALUES ($1, $2, 'RESUME', 'QUEUED', $3, now())
			RETURNING id::text, project_id::text, run_id::text, kind, state, wait_reason, idempotency_key, available_at, created_at, updated_at
		`, question.ProjectID, question.RunID, "question:"+question.ID+":resume"))
		if err != nil {
			return store.AnswerQuestionResult{}, err
		}
		result.Run = run
		result.Job = &job
	}

	if err := tx.Commit(ctx); err != nil {
		return store.AnswerQuestionResult{}, err
	}
	return result, nil
}

func validateQuestionAnswer(question store.Question, answer store.QuestionAnswer) error {
	if answer.Kind != question.Kind {
		return store.ErrInvalidArgument
	}
	switch question.Kind {
	case "TEXT":
		if answer.Text == nil || strings.TrimSpace(*answer.Text) == "" || len(answer.OptionIDs) != 0 {
			return store.ErrInvalidArgument
		}
	case "SINGLE_CHOICE", "MULTI_CHOICE":
		if answer.Text != nil || len(answer.OptionIDs) == 0 || (question.Kind == "SINGLE_CHOICE" && len(answer.OptionIDs) != 1) {
			return store.ErrInvalidArgument
		}
		var options []store.QuestionOption
		if err := json.Unmarshal(question.Options, &options); err != nil {
			return store.ErrInvalidArgument
		}
		allowed := make(map[string]struct{}, len(options))
		for _, option := range options {
			allowed[option.ID] = struct{}{}
		}
		seen := make(map[string]struct{}, len(answer.OptionIDs))
		for _, optionID := range answer.OptionIDs {
			if _, ok := allowed[optionID]; !ok {
				return store.ErrInvalidArgument
			}
			if _, duplicate := seen[optionID]; duplicate {
				return store.ErrInvalidArgument
			}
			seen[optionID] = struct{}{}
		}
	default:
		return store.ErrInvalidArgument
	}
	return nil
}

func questionAnswerOutcome(answer store.QuestionAnswer) string {
	if answer.Text != nil {
		return *answer.Text
	}
	encoded, _ := json.Marshal(answer.OptionIDs)
	return string(encoded)
}
