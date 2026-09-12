-- Phase 4 forward upgrade for persisted pre-v0.1 installations.
-- Safe to run repeatedly. Fresh installations still use schema.sql first.

BEGIN;

CREATE TABLE IF NOT EXISTS project_user_access (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    role text NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX IF NOT EXISTS project_user_access_user_idx
    ON project_user_access (user_id, project_id);

CREATE TABLE IF NOT EXISTS project_group_access (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    PRIMARY KEY (project_id, group_id)
);

CREATE INDEX IF NOT EXISTS project_group_access_group_idx
    ON project_group_access (group_id, project_id);

-- Phase 3 Projects predate Project access grants. Give every legacy Project a
-- qualifying direct admin using the oldest active deployment administrator.
-- Existing grants are preserved; a row is only inserted/promoted for Projects
-- that otherwise have no active direct User admin.
WITH fallback_admin AS (
    SELECT id
    FROM users
    WHERE status = 'active' AND deployment_role = 'admin'
    ORDER BY created_at, id
    LIMIT 1
),
projects_missing_active_direct_admin AS (
    SELECT p.id AS project_id
    FROM projects p
    WHERE NOT EXISTS (
        SELECT 1
        FROM project_user_access pua
        JOIN users u ON u.id = pua.user_id
        WHERE pua.project_id = p.id
          AND pua.role = 'admin'
          AND u.status = 'active'
    )
)
INSERT INTO project_user_access (project_id, user_id, role)
SELECT missing.project_id, fallback.id, 'admin'
FROM projects_missing_active_direct_admin missing
CROSS JOIN fallback_admin fallback
ON CONFLICT (project_id, user_id) DO UPDATE SET role = 'admin';

-- Never complete an upgrade that leaves a Project outside the Phase 4
-- invariant. A persisted installation with Projects but no active deployment
-- admin needs an administrator re-enabled before this upgrade can proceed.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM projects p
        WHERE NOT EXISTS (
            SELECT 1
            FROM project_user_access pua
            JOIN users u ON u.id = pua.user_id
            WHERE pua.project_id = p.id
              AND pua.role = 'admin'
              AND u.status = 'active'
        )
    ) THEN
        RAISE EXCEPTION 'Phase 4 upgrade requires every existing Project to have an active direct User admin; enable an active deployment admin and retry';
    END IF;
END
$$;

COMMIT;
