BEGIN;

SELECT pg_advisory_xact_lock(hashtextextended('agent-board:board-order-v1', 0));

DO $migration$
DECLARE
    legacy_schema boolean;
BEGIN
    SELECT NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'issues'
          AND column_name = 'board_position'
    ) INTO legacy_schema;

    IF NOT legacy_schema THEN
        RETURN;
    END IF;

    ALTER TABLE issues ADD COLUMN board_position bigint;

    WITH ranked AS (
        SELECT
            id,
            row_number() OVER (
                PARTITION BY project_id, status
                ORDER BY created_at, id
            ) - 1 AS board_position
        FROM issues
    )
    UPDATE issues AS issue
    SET board_position = ranked.board_position
    FROM ranked
    WHERE ranked.id = issue.id;

    ALTER TABLE issues ALTER COLUMN board_position SET DEFAULT 0;
    ALTER TABLE issues ALTER COLUMN board_position SET NOT NULL;
    ALTER TABLE issues
        ADD CONSTRAINT issues_board_position_nonnegative CHECK (board_position >= 0);
    CREATE INDEX issues_project_board_idx ON issues (project_id, status, board_position, id);

    UPDATE projects
    SET workflow_settings = jsonb_set(workflow_settings, '{strictOrder}', 'false'::jsonb, true),
        updated_at = now();
    ALTER TABLE projects
        ADD CONSTRAINT projects_strict_order_type_check CHECK (
            NOT (workflow_settings ? 'strictOrder')
            OR jsonb_typeof(workflow_settings->'strictOrder') = 'boolean'
        );

    EXECUTE $function$
        CREATE OR REPLACE FUNCTION default_project_workflow_settings() RETURNS trigger AS $body$
        BEGIN
            IF NOT (NEW.workflow_settings ? 'strictOrder') THEN
                NEW.workflow_settings = jsonb_set(NEW.workflow_settings, '{strictOrder}', 'true'::jsonb, true);
            END IF;
            RETURN NEW;
        END;
        $body$ LANGUAGE plpgsql
    $function$;

    CREATE TRIGGER projects_workflow_defaults
        BEFORE INSERT ON projects
        FOR EACH ROW EXECUTE FUNCTION default_project_workflow_settings();
END
$migration$;

COMMIT;
