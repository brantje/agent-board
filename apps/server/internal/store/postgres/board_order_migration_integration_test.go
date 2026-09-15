package postgres

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBoardOrderMigrationBackfillsLegacyDataDeterministically(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		DROP TRIGGER projects_workflow_defaults ON projects;
		DROP FUNCTION default_project_workflow_settings();
		ALTER TABLE issues DROP COLUMN board_position CASCADE;
	`); err != nil {
		t.Fatal(err)
	}

	var projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, issue_prefix, repository_path, workflow_settings)
		VALUES ('legacy-board-order', 'LBO', '/repo/legacy-board-order', '{"strictOrder":true,"other":"kept"}'::jsonb)
		RETURNING id::text
	`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}

	issueIDs := []string{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004",
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO issues (id, project_id, number, title, status, created_at) VALUES
		($2, $1, 1, 'todo later id', 'TODO', '2026-01-02T00:00:00Z'),
		($3, $1, 2, 'todo first',    'TODO', '2026-01-01T00:00:00Z'),
		($4, $1, 3, 'todo tie',      'TODO', '2026-01-02T00:00:00Z'),
		($5, $1, 4, 'review first',  'REVIEW', '2026-01-03T00:00:00Z')
	`, projectID, issueIDs[2], issueIDs[0], issueIDs[3], issueIDs[1]); err != nil {
		t.Fatal(err)
	}

	if err := runBoardOrderMigration(ctx, pool); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT id::text, status, board_position
		FROM issues
		WHERE project_id=$1
		ORDER BY status, board_position, id
	`, projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	positions := map[string]int64{}
	for rows.Next() {
		var id, status string
		var position int64
		if err := rows.Scan(&id, &status, &position); err != nil {
			t.Fatal(err)
		}
		positions[id] = position
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if positions[issueIDs[0]] != 0 || positions[issueIDs[2]] != 1 || positions[issueIDs[3]] != 2 {
		t.Fatalf("TODO positions=%v want deterministic created_at/id order", positions)
	}
	if positions[issueIDs[1]] != 0 {
		t.Fatalf("REVIEW position=%d want 0", positions[issueIDs[1]])
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
		t.Fatalf("legacy strictOrder=%v want false", workflow["strictOrder"])
	}
	if workflow["other"] != "kept" {
		t.Fatalf("migration lost unrelated workflow setting: %v", workflow)
	}

	var newSettings json.RawMessage
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, issue_prefix, repository_path, workflow_settings)
		VALUES ('post-migration-project', 'PMP', '/repo/post-migration-project', '{}'::jsonb)
		RETURNING workflow_settings
	`).Scan(&newSettings); err != nil {
		t.Fatal(err)
	}
	workflow = nil
	if err := json.Unmarshal(newSettings, &workflow); err != nil {
		t.Fatal(err)
	}
	if strict, ok := workflow["strictOrder"].(bool); !ok || !strict {
		t.Fatalf("new project strictOrder=%v want true", workflow["strictOrder"])
	}

	if _, err := pool.Exec(ctx, `UPDATE projects SET workflow_settings=jsonb_set(workflow_settings, '{strictOrder}', 'true'::jsonb) WHERE id=$1`, projectID); err != nil {
		t.Fatal(err)
	}
	if err := runBoardOrderMigration(ctx, pool); err != nil {
		t.Fatalf("rerun migration: %v", err)
	}
	var strict bool
	if err := pool.QueryRow(ctx, `SELECT (workflow_settings->>'strictOrder')::boolean FROM projects WHERE id=$1`, projectID).Scan(&strict); err != nil {
		t.Fatal(err)
	}
	if !strict {
		t.Fatal("idempotent migration reset an explicitly changed strictOrder value")
	}
}
