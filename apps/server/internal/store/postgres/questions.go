package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
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
