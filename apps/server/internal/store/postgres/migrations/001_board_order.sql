BEGIN;

SELECT pg_advisory_xact_lock(hashtextextended('agent-board:board-order-v1', 0));

DO $migration$
DECLARE
    board_column_exists boolean;
    missing_positions boolean;
BEGIN
    IF to_regclass('public.issues') IS NULL THEN
        RETURN;
    END IF;

    SELECT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'issues'
          AND column_name = 'board_position'
    ) INTO board_column_exists;

    IF NOT board_column_exists THEN
        ALTER TABLE issues ADD COLUMN board_position bigint;
    END IF;

    SELECT EXISTS (SELECT 1 FROM issues WHERE board_position IS NULL)
    INTO missing_positions;

    IF NOT board_column_exists OR missing_positions THEN
        WITH ranked AS (
            SELECT
                id,
                row_number() OVER (
                    PARTITION BY project_id, status
                    ORDER BY board_position NULLS LAST, created_at, id
                ) - 1 AS board_position
            FROM issues
        )
        UPDATE issues AS issue
        SET board_position = ranked.board_position
        FROM ranked
        WHERE ranked.id = issue.id;
    END IF;

    ALTER TABLE issues ALTER COLUMN board_position SET DEFAULT 0;
    ALTER TABLE issues ALTER COLUMN board_position SET NOT NULL;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'issues'::regclass
          AND conname IN ('issues_board_position_nonnegative', 'issues_board_position_check')
    ) THEN
        ALTER TABLE issues
            ADD CONSTRAINT issues_board_position_nonnegative CHECK (board_position >= 0);
    END IF;

    CREATE INDEX IF NOT EXISTS issues_project_board_idx
        ON issues (project_id, status, board_position, id);

    IF NOT board_column_exists THEN
        UPDATE projects
        SET workflow_settings = jsonb_set(workflow_settings, '{strictOrder}', 'false'::jsonb, true),
            updated_at = now();
    ELSE
        UPDATE projects
        SET workflow_settings = jsonb_set(workflow_settings, '{strictOrder}', 'false'::jsonb, true),
            updated_at = now()
        WHERE NOT (workflow_settings ? 'strictOrder');
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'projects'::regclass
          AND conname = 'projects_strict_order_type_check'
    ) THEN
        ALTER TABLE projects
            ADD CONSTRAINT projects_strict_order_type_check CHECK (
                NOT (workflow_settings ? 'strictOrder')
                OR jsonb_typeof(workflow_settings->'strictOrder') = 'boolean'
            );
    END IF;

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

    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'projects'::regclass
          AND tgname = 'projects_workflow_defaults'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER projects_workflow_defaults
            BEFORE INSERT ON projects
            FOR EACH ROW EXECUTE FUNCTION default_project_workflow_settings();
    END IF;
END
$migration$;

COMMIT;
