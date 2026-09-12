-- Agent Board v0.1 canonical pre-release PostgreSQL schema.
--
-- This file is intentionally the single source of truth before v0.1. Development
-- databases may be recreated when this schema changes incompatibly.

BEGIN;

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (btrim(name) <> ''),
    issue_prefix text NOT NULL CHECK (issue_prefix ~ '^[A-Z][A-Z0-9]{1,9}$'),
    next_issue_number integer NOT NULL DEFAULT 1 CHECK (next_issue_number >= 1),
    source_type text NOT NULL DEFAULT 'local' CHECK (source_type IN ('local', 'git')),
    clone_url text CHECK (clone_url IS NULL OR btrim(clone_url) <> ''),
    source_ref text CHECK (source_ref IS NULL OR btrim(source_ref) <> ''),
    repository_path text NOT NULL DEFAULT '',
    default_branch text NOT NULL DEFAULT 'main',
    workflow_settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(workflow_settings) = 'object'),
    allow_internal_runner boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (source_type = 'local' AND btrim(repository_path) <> '' AND btrim(default_branch) <> '' AND clone_url IS NULL AND source_ref IS NULL)
        OR
        (source_type = 'git' AND clone_url IS NOT NULL AND btrim(clone_url) <> '' AND repository_path = '' AND default_branch = '')
    )
);

CREATE UNIQUE INDEX projects_name_uq ON projects (lower(name));
CREATE UNIQUE INDEX projects_issue_prefix_uq ON projects (issue_prefix);

CREATE TABLE runners (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text CHECK (name IS NULL OR btrim(name) <> ''),
    token_hash bytea CHECK (token_hash IS NULL OR octet_length(token_hash) = 32),
    registration_token_hash bytea CHECK (registration_token_hash IS NULL OR octet_length(registration_token_hash) = 32),
    internal boolean NOT NULL DEFAULT false,
    registered_at timestamptz,
    revoked_at timestamptz,
    deleted_at timestamptz,
    last_seen_at timestamptz,
    capabilities jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(capabilities) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (deleted_at IS NULL OR revoked_at IS NOT NULL),
    CHECK (NOT internal OR (registered_at IS NOT NULL AND registration_token_hash IS NULL AND revoked_at IS NULL AND deleted_at IS NULL)),
    CHECK (
        (registered_at IS NULL AND NOT internal AND name IS NULL AND token_hash IS NULL) OR
        (registered_at IS NOT NULL AND name IS NOT NULL AND token_hash IS NOT NULL AND registration_token_hash IS NULL)
    ),
    CHECK (registered_at IS NOT NULL OR registration_token_hash IS NOT NULL OR revoked_at IS NOT NULL)
);
CREATE UNIQUE INDEX runners_active_name_uq ON runners (lower(name)) WHERE deleted_at IS NULL AND registered_at IS NOT NULL;
CREATE UNIQUE INDEX runners_registration_token_uq ON runners (registration_token_hash) WHERE registration_token_hash IS NOT NULL;
CREATE UNIQUE INDEX runners_internal_uq ON runners (internal) WHERE internal;

CREATE TABLE project_runners (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    runner_id uuid NOT NULL REFERENCES runners(id) ON DELETE RESTRICT,
    PRIMARY KEY (project_id, runner_id)
);

CREATE TABLE secrets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    ref text NOT NULL CHECK (btrim(ref) <> ''),
    ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) > 0),
    key_version integer NOT NULL CHECK (key_version >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX secrets_global_ref_uq ON secrets (ref) WHERE project_id IS NULL;
CREATE UNIQUE INDEX secrets_project_ref_uq ON secrets (project_id, ref) WHERE project_id IS NOT NULL;

CREATE TABLE providers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    kind text NOT NULL CHECK (btrim(kind) <> ''),
    base_url text,
    credential_ref text,
    enabled boolean NOT NULL DEFAULT true,
    health_status text NOT NULL DEFAULT 'UNKNOWN' CHECK (health_status IN ('UNKNOWN', 'HEALTHY', 'UNHEALTHY')),
    safe_metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(safe_metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX providers_global_name_uq ON providers (lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX providers_project_name_uq ON providers (project_id, lower(name)) WHERE project_id IS NOT NULL;

CREATE TABLE model_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    provider_id uuid NOT NULL REFERENCES providers(id) ON DELETE RESTRICT,
    name text NOT NULL CHECK (btrim(name) <> ''),
    model text NOT NULL CHECK (btrim(model) <> ''),
    temperature double precision CHECK (temperature IS NULL OR (temperature >= 0 AND temperature <= 2)),
    max_tokens integer CHECK (max_tokens IS NULL OR max_tokens >= 1),
    max_concurrent integer CHECK (max_concurrent IS NULL OR max_concurrent >= 1),
    generation_settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(generation_settings) = 'object'),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX model_profiles_global_name_uq ON model_profiles (lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX model_profiles_project_name_uq ON model_profiles (project_id, lower(name)) WHERE project_id IS NOT NULL;
CREATE INDEX model_profiles_provider_idx ON model_profiles (provider_id);

CREATE TABLE agents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    role_instructions text NOT NULL DEFAULT '',
    engine text NOT NULL CHECK (btrim(engine) <> ''),
    model_profile_id uuid NOT NULL REFERENCES model_profiles(id) ON DELETE RESTRICT,
    engine_settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(engine_settings) = 'object'),
    concurrency_limit integer NOT NULL DEFAULT 1 CHECK (concurrency_limit >= 1),
    state text NOT NULL DEFAULT 'ENABLED' CHECK (state IN ('DRAFT', 'ENABLED', 'DISABLED', 'ARCHIVED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX agents_global_name_uq ON agents (lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX agents_project_name_uq ON agents (project_id, lower(name)) WHERE project_id IS NOT NULL;
CREATE INDEX agents_model_profile_idx ON agents (model_profile_id);

CREATE TABLE issues (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number integer NOT NULL CHECK (number >= 1),
    title text NOT NULL CHECK (btrim(title) <> ''),
    description text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'BACKLOG' CHECK (status IN ('BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE')),
    priority integer NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
    assigned_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id),
    UNIQUE (project_id, number)
);

CREATE INDEX issues_project_status_idx ON issues (project_id, status, created_at);
CREATE INDEX issues_assigned_agent_idx ON issues (assigned_agent_id) WHERE assigned_agent_id IS NOT NULL;

CREATE TABLE issue_relationships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    source_issue_id uuid NOT NULL,
    target_issue_id uuid NOT NULL,
    type text NOT NULL CHECK (type IN ('blocks', 'depends_on', 'related_to', 'duplicates')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_relationships_source_fk FOREIGN KEY (project_id, source_issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_relationships_target_fk FOREIGN KEY (project_id, target_issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CHECK (source_issue_id <> target_issue_id),
    UNIQUE (project_id, source_issue_id, target_issue_id, type)
);

CREATE INDEX issue_relationships_source_idx ON issue_relationships (project_id, source_issue_id, created_at, id);

CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    path text NOT NULL CHECK (btrim(path) <> ''),
    repository_path text,
    base_branch text,
    base_revision text,
    working_branch text NOT NULL CHECK (btrim(working_branch) <> ''),
    current_branch text CHECK (current_branch IS NULL OR btrim(current_branch) <> ''),
    current_revision text CHECK (current_revision IS NULL OR btrim(current_revision) <> ''),
    bootstrap_status text NOT NULL DEFAULT 'PENDING' CHECK (bootstrap_status IN ('PENDING', 'READY', 'FAILED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workspaces_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    UNIQUE (issue_id),
    UNIQUE (project_id, id),
    UNIQUE (project_id, issue_id, id),
    UNIQUE (path)
);

CREATE INDEX workspaces_project_idx ON workspaces (project_id);

CREATE TABLE runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    agent_id uuid REFERENCES agents(id) ON DELETE RESTRICT,
    attempt integer NOT NULL CHECK (attempt >= 1),
    status text NOT NULL DEFAULT 'QUEUED' CHECK (status IN ('QUEUED', 'STARTING', 'RUNNING', 'WAITING_FOR_INPUT', 'PAUSED', 'READY_FOR_REVIEW', 'COMPLETED', 'FAILED', 'CANCELLED')),
    queue_reason text,
    failure_reason text,
    event_sequence bigint NOT NULL DEFAULT 0 CHECK (event_sequence >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT runs_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT runs_workspace_fk FOREIGN KEY (project_id, issue_id, workspace_id) REFERENCES workspaces(project_id, issue_id, id) ON DELETE RESTRICT,
    UNIQUE (issue_id, attempt),
    UNIQUE (project_id, id)
);

CREATE INDEX runs_project_status_idx ON runs (project_id, status, created_at);
CREATE INDEX runs_issue_created_idx ON runs (issue_id, created_at DESC);
CREATE INDEX runs_agent_active_idx ON runs (agent_id, status) WHERE agent_id IS NOT NULL AND status IN ('STARTING', 'RUNNING');

CREATE TABLE scheduler_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    run_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('START', 'RESUME')),
    state text NOT NULL DEFAULT 'QUEUED' CHECK (state IN ('QUEUED', 'CLAIMED', 'DONE', 'CANCELLED', 'FAILED')),
    wait_reason text,
    idempotency_key text NOT NULL CHECK (btrim(idempotency_key) <> ''),
    available_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT scheduler_jobs_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (project_id, id),
    UNIQUE (idempotency_key)
);

CREATE INDEX scheduler_jobs_claim_idx ON scheduler_jobs (state, available_at, created_at) WHERE state = 'QUEUED';
CREATE INDEX scheduler_jobs_run_idx ON scheduler_jobs (run_id, created_at DESC);

CREATE TABLE scheduler_leases (
    job_id uuid PRIMARY KEY REFERENCES scheduler_jobs(id) ON DELETE CASCADE,
    owner_id text NOT NULL CHECK (btrim(owner_id) <> ''),
    lease_token uuid NOT NULL DEFAULT gen_random_uuid(),
    acquired_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > acquired_at),
    UNIQUE (lease_token)
);

CREATE INDEX scheduler_leases_expiry_idx ON scheduler_leases (expires_at);

CREATE TABLE scheduler_capacity_reservations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    job_id uuid NOT NULL REFERENCES scheduler_jobs(id) ON DELETE CASCADE,
    run_id uuid NOT NULL,
    resource_kind text NOT NULL CHECK (resource_kind IN ('AGENT', 'MODEL_PROFILE', 'RUNNER')),
    resource_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT scheduler_capacity_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (job_id, resource_kind)
);

CREATE INDEX scheduler_capacity_resource_idx ON scheduler_capacity_reservations (resource_kind, resource_id);
CREATE INDEX scheduler_capacity_run_idx ON scheduler_capacity_reservations (run_id);

CREATE TABLE execution_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    run_id uuid NOT NULL,
    runner_id uuid NOT NULL REFERENCES runners(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'STARTING', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED')),
    cwd text NOT NULL DEFAULT '/workspace' CHECK (btrim(cwd) <> ''),
    command_argv jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(command_argv) = 'array'),
    exit_code integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT execution_sessions_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (project_id, id)
);

CREATE INDEX execution_sessions_run_idx ON execution_sessions (run_id, created_at);
CREATE INDEX execution_sessions_runner_idx ON execution_sessions (runner_id, created_at);

CREATE TABLE questions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    run_id uuid NOT NULL,
    prompt text NOT NULL CHECK (btrim(prompt) <> ''),
    kind text NOT NULL DEFAULT 'TEXT' CHECK (kind IN ('TEXT', 'SINGLE_CHOICE', 'MULTI_CHOICE')),
    options jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(options) = 'array'),
    recommendation text,
    custom boolean NOT NULL DEFAULT false,
    blocking boolean NOT NULL DEFAULT true,
    status text NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'ANSWERED', 'CANCELLED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    answered_at timestamptz,
    CONSTRAINT questions_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT questions_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (project_id, id),
    UNIQUE (project_id, run_id, id)
);

CREATE INDEX questions_open_idx ON questions (project_id, status, created_at) WHERE status = 'OPEN';
CREATE INDEX questions_run_idx ON questions (run_id, created_at);

CREATE TABLE engine_question_bindings (
    question_id uuid NOT NULL,
    project_id uuid NOT NULL,
    run_id uuid NOT NULL,
    engine text NOT NULL CHECK (btrim(engine) <> ''),
    correlation_key text NOT NULL CHECK (btrim(correlation_key) <> ''),
    state text NOT NULL DEFAULT 'OPEN' CHECK (state IN ('OPEN', 'ANSWERED', 'RESOLVED', 'CANCELLED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT engine_question_bindings_question_fk FOREIGN KEY (project_id, run_id, question_id) REFERENCES questions(project_id, run_id, id) ON DELETE CASCADE,
    CONSTRAINT engine_question_bindings_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    PRIMARY KEY (question_id),
    UNIQUE (project_id, run_id, engine, correlation_key)
);

CREATE INDEX engine_question_bindings_run_state_idx ON engine_question_bindings (project_id, run_id, state);

CREATE TABLE decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id uuid,
    run_id uuid,
    question_id uuid REFERENCES questions(id) ON DELETE SET NULL,
    kind text NOT NULL CHECK (btrim(kind) <> ''),
    outcome text NOT NULL CHECK (btrim(outcome) <> ''),
    actor_type text NOT NULL CHECK (actor_type IN ('HUMAN', 'SYSTEM', 'AGENT')),
    actor_id text,
    safe_details jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(safe_details) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT decisions_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT decisions_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (project_id, id)
);

CREATE INDEX decisions_run_idx ON decisions (run_id, created_at) WHERE run_id IS NOT NULL;
CREATE INDEX decisions_issue_idx ON decisions (issue_id, created_at) WHERE issue_id IS NOT NULL;

CREATE TABLE reviews (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    run_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'APPROVED', 'CHANGES_REQUESTED', 'CANCELLED')),
    decision_id uuid REFERENCES decisions(id) ON DELETE SET NULL,
    base_revision text CHECK (base_revision IS NULL OR btrim(base_revision) <> ''),
    review_revision text CHECK (review_revision IS NULL OR btrim(review_revision) <> ''),
    requested_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reviews_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT reviews_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    CHECK ((base_revision IS NULL) = (review_revision IS NULL)),
    UNIQUE (run_id),
    UNIQUE (project_id, id)
);

CREATE INDEX reviews_pending_idx ON reviews (project_id, requested_at) WHERE status = 'PENDING';

CREATE TABLE run_provenance (
    run_id uuid PRIMARY KEY REFERENCES runs(id) ON DELETE RESTRICT,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    snapshot jsonb NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT run_provenance_run_scope_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE RESTRICT
);

CREATE TABLE events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    schema_version integer NOT NULL DEFAULT 1 CHECK (schema_version >= 1),
    type text NOT NULL CHECK (btrim(type) <> ''),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    issue_id uuid,
    run_id uuid,
    agent_id uuid REFERENCES agents(id) ON DELETE RESTRICT,
    workspace_id uuid,
    correlation_id uuid,
    parent_event_id uuid REFERENCES events(id) ON DELETE RESTRICT,
    sequence bigint,
    actor jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(actor) = 'object'),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT events_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT events_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT events_workspace_fk FOREIGN KEY (project_id, workspace_id) REFERENCES workspaces(project_id, id) ON DELETE RESTRICT,
    CHECK ((run_id IS NULL AND sequence IS NULL) OR (run_id IS NOT NULL AND sequence IS NOT NULL AND sequence >= 1)),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX events_run_sequence_uq ON events (run_id, sequence) WHERE run_id IS NOT NULL;
CREATE INDEX events_run_timeline_idx ON events (run_id, sequence) WHERE run_id IS NOT NULL;
CREATE INDEX events_project_timeline_idx ON events (project_id, created_at, id);
CREATE INDEX events_correlation_idx ON events (correlation_id) WHERE correlation_id IS NOT NULL;
CREATE INDEX events_question_id_lookup_idx ON events (project_id, run_id, type, (payload->>'questionId'));

CREATE TABLE raw_output_chunks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    run_id uuid NOT NULL,
    stream text NOT NULL CHECK (stream IN ('STDOUT', 'STDERR', 'PROTOCOL', 'DIAGNOSTIC')),
    sequence bigint NOT NULL CHECK (sequence >= 1),
    storage_ref text NOT NULL CHECK (btrim(storage_ref) <> ''),
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    digest text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT raw_output_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT raw_output_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (run_id, stream, sequence),
    UNIQUE (project_id, id)
);

CREATE INDEX raw_output_run_idx ON raw_output_chunks (run_id, stream, sequence);

CREATE TABLE artifacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    run_id uuid NOT NULL,
    name text NOT NULL CHECK (btrim(name) <> ''),
    kind text NOT NULL CHECK (btrim(kind) <> ''),
    media_type text,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    digest text,
    storage_ref text NOT NULL CHECK (btrim(storage_ref) <> ''),
    safe_metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(safe_metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT artifacts_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT artifacts_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (project_id, id)
);

CREATE INDEX artifacts_run_idx ON artifacts (run_id, created_at) WHERE deleted_at IS NULL;

-- Global configuration can be consumed by any Project; Project-owned configuration
-- may only reference other global configuration or configuration owned by that Project.
CREATE FUNCTION enforce_configuration_scope() RETURNS trigger AS $$
DECLARE
    referenced_project_id uuid;
BEGIN
    IF TG_TABLE_NAME = 'agents' THEN
        SELECT project_id INTO referenced_project_id FROM model_profiles WHERE id = NEW.model_profile_id;
        IF referenced_project_id IS NOT NULL AND referenced_project_id IS DISTINCT FROM NEW.project_id THEN
            RAISE EXCEPTION 'agent cannot reference model profile from another project' USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'issues' THEN
        IF NEW.assigned_agent_id IS NOT NULL THEN
            SELECT project_id INTO referenced_project_id FROM agents WHERE id = NEW.assigned_agent_id;
            IF referenced_project_id IS NOT NULL AND referenced_project_id IS DISTINCT FROM NEW.project_id THEN
                RAISE EXCEPTION 'issue cannot reference agent from another project' USING ERRCODE = '23514';
            END IF;
        END IF;
    ELSIF TG_TABLE_NAME = 'runs' THEN
        IF NEW.agent_id IS NOT NULL THEN
            SELECT project_id INTO referenced_project_id FROM agents WHERE id = NEW.agent_id;
            IF referenced_project_id IS NOT NULL AND referenced_project_id IS DISTINCT FROM NEW.project_id THEN
                RAISE EXCEPTION 'run cannot reference agent from another project' USING ERRCODE = '23514';
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enforce_configuration_owner_change() RETURNS trigger AS $$
BEGIN
    IF NEW.project_id IS NOT DISTINCT FROM OLD.project_id OR NEW.project_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF TG_TABLE_NAME = 'model_profiles' THEN
        IF EXISTS (
            SELECT 1 FROM agents
            WHERE model_profile_id = NEW.id AND project_id IS DISTINCT FROM NEW.project_id
        ) THEN
            RAISE EXCEPTION 'model profile ownership change would cross project scope' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER model_profiles_owner_change_check BEFORE UPDATE OF project_id ON model_profiles FOR EACH ROW EXECUTE FUNCTION enforce_configuration_owner_change();

CREATE TRIGGER agents_scope_check BEFORE INSERT OR UPDATE OF project_id, model_profile_id ON agents FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();
CREATE TRIGGER issues_scope_check BEFORE INSERT OR UPDATE OF project_id, assigned_agent_id ON issues FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();
CREATE TRIGGER runs_scope_check BEFORE INSERT OR UPDATE OF project_id, agent_id ON runs FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();

CREATE FUNCTION reject_immutable_row_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER events_append_only
    BEFORE UPDATE OR DELETE ON events
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row_mutation();

CREATE TRIGGER run_provenance_immutable
    BEFORE UPDATE OR DELETE ON run_provenance
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row_mutation();

COMMIT;