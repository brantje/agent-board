package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestBoardOrderMigrationRepairsPartialUpgrade(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		DROP TRIGGER projects_workflow_defaults ON projects;
		DROP FUNCTION default_project_workflow_settings();
		ALTER TABLE projects DROP CONSTRAINT projects_strict_order_type_check;
		DROP INDEX issues_project_board_idx;
		ALTER TABLE issues DROP CONSTRAINT issues_board_position_nonnegative;
		ALTER TABLE issues ALTER COLUMN board_position DROP NOT NULL;
		ALTER TABLE issues ALTER COLUMN board_position DROP DEFAULT;
	`); err != nil {
		t.Fatal(err)
	}

	var projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, issue_prefix, repository_path, workflow_settings)
		VALUES ('partial-board-order', 'PBO', '/repo/partial-board-order', '{}'::jsonb)
		RETURNING id::text
	`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}

	issueIDs := []string{
		"00000000-0000-4000-8000-000000000011",
		"00000000-0000-4000-8000-000000000012",
		"00000000-0000-4000-8000-000000000013",
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO issues (id, project_id, number, title, status, board_position, created_at) VALUES
		($2, $1, 1, 'later stored position', 'TODO', 5,    '2026-01-01T00:00:00Z'),
		($3, $1, 2, 'missing position',      'TODO', NULL, '2026-01-02T00:00:00Z'),
		($4, $1, 3, 'earlier stored position','TODO', 2,    '2026-01-03T00:00:00Z')
	`, projectID, issueIDs[0], issueIDs[1], issueIDs[2]); err != nil {
		t.Fatal(err)
	}

	if err := runBoardOrderMigration(ctx, pool); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT id::text, board_position
		FROM issues
		WHERE project_id=$1
		ORDER BY board_position, id
	`, projectID)
	if err != nil {
		t.Fatal(err)
	}
	var ordered []string
	var positions []int64
	for rows.Next() {
		var id string
		var position int64
		if err := rows.Scan(&id, &position); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ordered = append(ordered, id)
		positions = append(positions, position)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{issueIDs[2], issueIDs[0], issueIDs[1]}
	for i := range wantOrder {
		if ordered[i] != wantOrder[i] || positions[i] != int64(i) {
			t.Fatalf("repaired order=%v positions=%v want=%v positions=[0 1 2]", ordered, positions, wantOrder)
		}
	}

	var nullable string
	var defaultValue *string
	if err := pool.QueryRow(ctx, `
		SELECT is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema='public' AND table_name='issues' AND column_name='board_position'
	`).Scan(&nullable, &defaultValue); err != nil {
		t.Fatal(err)
	}
	if nullable != "NO" || defaultValue == nil {
		t.Fatalf("board_position nullable=%q default=%v want NOT NULL with default", nullable, defaultValue)
	}

	var constraintExists, indexExists, triggerExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='issues'::regclass AND conname='issues_board_position_nonnegative')`).Scan(&constraintExists); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.issues_project_board_idx') IS NOT NULL`).Scan(&indexExists); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='projects'::regclass AND tgname='projects_workflow_defaults' AND NOT tgisinternal)`).Scan(&triggerExists); err != nil {
		t.Fatal(err)
	}
	if !constraintExists || !indexExists || !triggerExists {
		t.Fatalf("repaired schema constraint=%v index=%v trigger=%v", constraintExists, indexExists, triggerExists)
	}

	var settings json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT workflow_settings FROM projects WHERE id=$1`, projectID).Scan(&settings); err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := json.Unmarshal(settings, &workflow); err != nil {
		t.Fatal(err)
	}
	if strict, ok := workflow["strictOrder"].(bool); !ok || strict {
		t.Fatalf("partially upgraded project strictOrder=%v want false", workflow["strictOrder"])
	}

	var newStrict bool
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, issue_prefix, repository_path, workflow_settings)
		VALUES ('post-partial-board-order', 'PPB', '/repo/post-partial-board-order', '{}'::jsonb)
		RETURNING (workflow_settings->>'strictOrder')::boolean
	`).Scan(&newStrict); err != nil {
		t.Fatal(err)
	}
	if !newStrict {
		t.Fatal("new project strictOrder=false want true")
	}

	s := New(pool)
	result, err := s.PlaceIssue(ctx, store.IssueBoardPlacement{
		ProjectID: projectID,
		IssueID:   issueIDs[2],
		Status:    "TODO",
		Actor:     store.EmptyObject,
	})
	if err != nil {
		t.Fatalf("append after repaired migration: %v", err)
	}
	if result.Issue.BoardPosition != 2 {
		t.Fatalf("append boardPosition=%d want=2", result.Issue.BoardPosition)
	}
}
