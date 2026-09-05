package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCanonicalSchemaCreatesRequiredTables(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	required := []string{
		"projects", "issues", "providers", "model_profiles", "runtimes",
		"executor_profiles", "agents", "workspaces", "runs", "scheduler_jobs",
		"scheduler_leases", "scheduler_capacity_reservations", "runtime_instances",
		"execution_sessions", "questions", "decisions", "reviews", "run_provenance",
		"events", "raw_output_chunks", "artifacts",
	}

	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public'`)
	if err != nil {
		t.Fatalf("list public tables: %v", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		seen[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate public tables: %v", err)
	}

	for _, name := range required {
		if !seen[name] {
			t.Errorf("required table %q is missing", name)
		}
	}
}

func TestSchemaEnforcesWorkspaceAndRuntimeIdentity(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	projectID := insertProject(t, pool, "project-a")
	issueID := insertIssue(t, pool, projectID, "Issue A")
	workspaceID := insertWorkspace(t, pool, projectID, issueID)

	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (project_id, issue_id, path, working_branch) VALUES ($1, $2, '/tmp/duplicate', 'issue/duplicate')`, projectID, issueID); err == nil {
		t.Fatal("expected second Workspace for one Issue to be rejected")
	}

	runtimeID := insertRuntime(t, pool, nil, "global-runtime")
	instanceID := insertRuntimeInstance(t, pool, projectID, workspaceID, runtimeID)
	otherIssueID := insertIssue(t, pool, projectID, "Issue B")
	otherWorkspaceID := insertWorkspace(t, pool, projectID, otherIssueID)

	if _, err := pool.Exec(ctx, `UPDATE runtime_instances SET workspace_id = $1 WHERE id = $2`, otherWorkspaceID, instanceID); err == nil {
		t.Fatal("expected Runtime Instance Workspace rebinding to be rejected")
	}

	var runIDColumnCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'runtime_instances' AND column_name = 'run_id'
	`).Scan(&runIDColumnCount); err != nil {
		t.Fatalf("inspect runtime_instances columns: %v", err)
	}
	if runIDColumnCount != 0 {
		t.Fatal("runtime_instances must not durably bind to a Run")
	}
}

func TestSchemaRejectsCrossProjectExecutionReferences(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	projectA := insertProject(t, pool, "project-a")
	projectB := insertProject(t, pool, "project-b")
	issueA := insertIssue(t, pool, projectA, "Issue A")
	issueB := insertIssue(t, pool, projectB, "Issue B")
	workspaceA := insertWorkspace(t, pool, projectA, issueA)
	workspaceB := insertWorkspace(t, pool, projectB, issueB)

	if _, err := pool.Exec(ctx, `INSERT INTO runs (project_id, issue_id, workspace_id, attempt, status) VALUES ($1, $2, $3, 1, 'QUEUED')`, projectA, issueB, workspaceA); err == nil {
		t.Fatal("expected cross-Project Issue reference to be rejected")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (project_id, issue_id, workspace_id, attempt, status) VALUES ($1, $2, $3, 1, 'QUEUED')`, projectA, issueA, workspaceB); err == nil {
		t.Fatal("expected cross-Project Workspace reference to be rejected")
	}
}

func TestSchemaEnforcesAppendOnlyEvents(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	projectID := insertProject(t, pool, "project-a")
	issueID := insertIssue(t, pool, projectID, "Issue A")
	workspaceID := insertWorkspace(t, pool, projectID, issueID)
	var runID string
	if err := pool.QueryRow(ctx, `INSERT INTO runs (project_id, issue_id, workspace_id, attempt, status) VALUES ($1, $2, $3, 1, 'QUEUED') RETURNING id::text`, projectID, issueID, workspaceID).Scan(&runID); err != nil {
		t.Fatalf("insert Run: %v", err)
	}

	var eventID string
	if err := pool.QueryRow(ctx, `INSERT INTO events (project_id, issue_id, run_id, sequence, schema_version, type, actor, payload) VALUES ($1, $2, $3, 1, 1, 'run.created', '{}'::jsonb, '{}'::jsonb) RETURNING id::text`, projectID, issueID, runID).Scan(&eventID); err != nil {
		t.Fatalf("insert Event: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE events SET type = 'run.started' WHERE id = $1`, eventID); err == nil {
		t.Fatal("expected Event update to be rejected")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM events WHERE id = $1`, eventID); err == nil {
		t.Fatal("expected Event delete to be rejected")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO events (project_id, issue_id, run_id, sequence, schema_version, type, actor, payload) VALUES ($1, $2, $3, 1, 1, 'run.queued', '{}'::jsonb, '{}'::jsonb)`, projectID, issueID, runID); err == nil {
		t.Fatal("expected duplicate per-Run Event sequence to be rejected")
	}
}

func TestSchemaHasNoObviousSecretPlaintextColumns(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
	`)
	if err != nil {
		t.Fatalf("inspect schema columns: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tableName, columnName string
		if err := rows.Scan(&tableName, &columnName); err != nil {
			t.Fatalf("scan schema column: %v", err)
		}
		lower := strings.ToLower(columnName)
		for _, forbidden := range []string{"password", "secret_value", "access_token", "api_key", "private_key"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("durable secret-like column found: %s.%s", tableName, columnName)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema columns: %v", err)
	}
}

func insertProject(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO projects (name, repository_path, default_branch) VALUES ($1, '/repo', 'main') RETURNING id::text`, name).Scan(&id); err != nil {
		t.Fatalf("insert Project: %v", err)
	}
	return id
}

func insertIssue(t *testing.T, pool *pgxpool.Pool, projectID, title string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO issues (project_id, title, status) VALUES ($1, $2, 'TODO') RETURNING id::text`, projectID, title).Scan(&id); err != nil {
		t.Fatalf("insert Issue: %v", err)
	}
	return id
}

func insertWorkspace(t *testing.T, pool *pgxpool.Pool, projectID, issueID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO workspaces (project_id, issue_id, path, working_branch) VALUES ($1, $2, $3, $4) RETURNING id::text`, projectID, issueID, "/workspace/"+issueID, "issue/"+issueID).Scan(&id); err != nil {
		t.Fatalf("insert Workspace: %v", err)
	}
	return id
}

func insertRuntime(t *testing.T, pool *pgxpool.Pool, projectID *string, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO runtimes (project_id, name, kind, image, network_policy, workspace_policy, enabled) VALUES ($1, $2, 'docker', 'agent-board/runtime:test', 'restricted', 'issue', true) RETURNING id::text`, projectID, name).Scan(&id); err != nil {
		t.Fatalf("insert Runtime: %v", err)
	}
	return id
}

func insertRuntimeInstance(t *testing.T, pool *pgxpool.Pool, projectID, workspaceID, runtimeID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO runtime_instances (project_id, workspace_id, runtime_id, status) VALUES ($1, $2, $3, 'PROVISIONING') RETURNING id::text`, projectID, workspaceID, runtimeID).Scan(&id); err != nil {
		t.Fatalf("insert Runtime Instance: %v", err)
	}
	return id
}
