package postgres

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectAccessUpgradePreservesPhase3StateAndBackfillsAdmin(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var adminID, projectID, groupID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (username, email, display_name, password_hash, deployment_role, status)
		VALUES ('legacy-admin', 'legacy-admin@example.com', 'Legacy Admin', 'legacy-hash', 'admin', 'active')
		RETURNING id
	`).Scan(&adminID); err != nil {
		t.Fatalf("insert legacy admin: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, issue_prefix, source_type, repository_path, default_branch)
		VALUES ('Legacy Project', 'LEG', 'local', '/repo/legacy', 'main')
		RETURNING id
	`).Scan(&projectID); err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO groups (name) VALUES ('legacy-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatalf("insert legacy group: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, groupID, adminID); err != nil {
		t.Fatalf("insert legacy group membership: %v", err)
	}

	// Recreate the persisted Phase 3 shape: all existing domain/auth/group data
	// remains, but the Phase 4 access tables do not exist yet.
	if _, err := pool.Exec(ctx, `DROP TABLE project_group_access; DROP TABLE project_user_access`); err != nil {
		t.Fatalf("remove Phase 4 tables from fixture: %v", err)
	}

	upgradePath := filepath.Join("..", "..", "..", "..", "..", "packages", "database", "upgrades", "004-project-access.sql")
	upgrade, err := os.ReadFile(upgradePath)
	if err != nil {
		t.Fatalf("read project access upgrade %s: %v", upgradePath, err)
	}
	if _, err := pool.Exec(ctx, string(upgrade)); err != nil {
		t.Fatalf("apply project access upgrade: %v", err)
	}
	// Compose runs this on every startup, so repeated execution must be safe.
	if _, err := pool.Exec(ctx, string(upgrade)); err != nil {
		t.Fatalf("reapply project access upgrade: %v", err)
	}

	var projectName, username, groupName string
	if err := pool.QueryRow(ctx, `SELECT name FROM projects WHERE id = $1`, projectID).Scan(&projectName); err != nil || projectName != "Legacy Project" {
		t.Fatalf("legacy project name=%q err=%v", projectName, err)
	}
	if err := pool.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, adminID).Scan(&username); err != nil || username != "legacy-admin" {
		t.Fatalf("legacy user username=%q err=%v", username, err)
	}
	if err := pool.QueryRow(ctx, `SELECT name FROM groups WHERE id = $1`, groupID).Scan(&groupName); err != nil || groupName != "legacy-group" {
		t.Fatalf("legacy group name=%q err=%v", groupName, err)
	}

	var role string
	if err := pool.QueryRow(ctx, `SELECT role FROM project_user_access WHERE project_id = $1 AND user_id = $2`, projectID, adminID).Scan(&role); err != nil {
		t.Fatalf("read backfilled direct admin: %v", err)
	}
	if role != "admin" {
		t.Fatalf("backfilled role=%q, want admin", role)
	}

	for _, index := range []string{"project_user_access_user_idx", "project_group_access_group_idx"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, index).Scan(&exists); err != nil {
			t.Fatalf("check index %s: %v", index, err)
		}
		if !exists {
			t.Fatalf("upgrade did not create index %s", index)
		}
	}
}

func TestProjectAccessUpgradeRejectsLegacyProjectsWithoutActiveAdmin(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (name, issue_prefix, source_type, repository_path, default_branch)
		VALUES ('Orphan Legacy Project', 'ORP', 'local', '/repo/orphan', 'main');
		DROP TABLE project_group_access;
		DROP TABLE project_user_access;
	`); err != nil {
		t.Fatalf("prepare orphan legacy fixture: %v", err)
	}

	upgradePath := filepath.Join("..", "..", "..", "..", "..", "packages", "database", "upgrades", "004-project-access.sql")
	upgrade, err := os.ReadFile(upgradePath)
	if err != nil {
		t.Fatalf("read project access upgrade: %v", err)
	}
	if _, err := pool.Exec(ctx, string(upgrade)); err == nil {
		t.Fatal("upgrade unexpectedly allowed a legacy Project without an active direct admin")
	}

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.project_user_access') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("check rolled-back table: %v", err)
	}
	if exists {
		t.Fatal("failed upgrade did not roll back project access table creation")
	}
}
