package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSchemaRejectsQuestionBindingToDifferentRun(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	projectID := insertProject(t, pool, "question-binding-project")
	issueID := insertIssue(t, pool, projectID, "Question binding issue")
	workspaceID := insertWorkspace(t, pool, projectID, issueID)

	var firstRunID, secondRunID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO runs (project_id, issue_id, workspace_id, attempt, status)
		VALUES ($1, $2, $3, 1, 'RUNNING')
		RETURNING id::text
	`, projectID, issueID, workspaceID).Scan(&firstRunID); err != nil {
		t.Fatalf("insert first Run: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO runs (project_id, issue_id, workspace_id, attempt, status)
		VALUES ($1, $2, $3, 2, 'RUNNING')
		RETURNING id::text
	`, projectID, issueID, workspaceID).Scan(&secondRunID); err != nil {
		t.Fatalf("insert second Run: %v", err)
	}

	var questionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO questions (project_id, issue_id, run_id, prompt, kind, blocking, status)
		VALUES ($1, $2, $3, 'Which Run owns this Question?', 'TEXT', true, 'OPEN')
		RETURNING id::text
	`, projectID, issueID, firstRunID).Scan(&questionID); err != nil {
		t.Fatalf("insert Question: %v", err)
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO engine_question_bindings (question_id, project_id, run_id, engine, correlation_key)
		VALUES ($1, $2, $3, 'opencode', 'cross-run-binding')
	`, questionID, projectID, secondRunID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("expected cross-Run Question binding to be rejected by a foreign-key violation, got %v", err)
	}
}
