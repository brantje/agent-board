-- Agent Board v0.1 canonical pre-release PostgreSQL schema.
--
-- This file is intentionally the single source of truth before v0.1. Development
-- databases may be recreated when this schema changes incompatibly.

BEGIN;

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (btrim(name) <> ''),
    repository_path text NOT NULL CHECK (btrim(repository_path) <> ''),
    default_branch text NOT NULL DEFAULT 'main' CHECK (btrim(default_branch) <> ''),
    workflow_settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(workflow_settings) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX projects_name_uq ON projects (lower(name));

CREATE TABLE providers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (btrim(name) <> ''),
    kind text NOT NULL CHECK (btrim(kind) <> ''),
    base_url text,
    credential_ref text,
    enabled boolean NOT NULL DEFAULT true,
    health_status text NOT NULL DEFAULT 'UNKNOWN' CHECK (health_status IN ('UNKNOWN', 'HEALTHY', 'UNHEALTHY')),
    safe_metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(safe_metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX providers_name_uq ON providers (lower(name));

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

CREATE TABLE runtimes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    kind text NOT NULL CHECK (kind IN ('docker')),
    image text NOT NULL CHECK (btrim(image) <> ''),
    cpu_limit_millis integer CHECK (cpu_limit_millis IS NULL OR cpu_limit_millis >= 1),
    memory_limit_bytes bigint CHECK (memory_limit_bytes IS NULL OR memory_limit_bytes >= 1),
    pid_limit integer CHECK (pid_limit IS NULL OR pid_limit >= 1),
    timeout_seconds integer CHECK (timeout_seconds IS NULL OR timeout_seconds >= 1),
    network_policy text NOT NULL CHECK (network_policy IN ('none', 'restricted', 'outbound')),
    workspace_policy text NOT NULL DEFAULT 'issue' CHECK (workspace_policy = 'issue'),
    allowed_secret_refs text[] NOT NULL DEFAULT ARRAY[]::text[],
    capabilities jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(capabilities) = 'object'),
    enabled boolean NOT NULL DEFAULT true,
    health_status text NOT NULL DEFAULT 'UNKNOWN' CHECK (health_status IN ('UNKNOWN', 'HEALTHY', 'UNHEALTHY')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX runtimes_global_name_uq ON runtimes (lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX runtimes_project_name_uq ON runtimes (project_id, lower(name)) WHERE project_id IS NOT NULL;

CREATE TABLE executor_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    engine text NOT NULL CHECK (btrim(engine) <> ''),
    model_profile_id uuid NOT NULL REFERENCES model_profiles(id) ON DELETE RESTRICT,
    runtime_id uuid NOT NULL REFERENCES runtimes(id) ON DELETE RESTRICT,
    engine_settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(engine_settings) = 'object'),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX executor_profiles_global_name_uq ON executor_profiles (lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX executor_profiles_project_name_uq ON executor_profiles (project_id, lower(name)) WHERE project_id IS NOT NULL;
CREATE INDEX executor_profiles_model_profile_idx ON executor_profiles (model_profile_id);
CREATE INDEX executor_profiles_runtime_idx ON executor_profiles (runtime_id);

CREATE TABLE agents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    role_instructions text NOT NULL DEFAULT '',
    executor_profile_id uuid NOT NULL REFERENCES executor_profiles(id) ON DELETE RESTRICT,
    concurrency_limit integer NOT NULL DEFAULT 1 CHECK (concurrency_limit >= 1),
    state text NOT NULL DEFAULT 'ENABLED' CHECK (state IN ('DRAFT', 'ENABLED', 'DISABLED', 'ARCHIVED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX agents_global_name_uq ON agents (lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX agents_project_name_uq ON agents (project_id, lower(name)) WHERE project_id IS NOT NULL;
CREATE INDEX agents_executor_profile_idx ON agents (executor_profile_id);

CREATE TABLE issues (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title text NOT NULL CHECK (btrim(title) <> ''),
    description text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'BACKLOG' CHECK (status IN ('BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE')),
    assigned_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id)
);

CREATE INDEX issues_project_status_idx ON issues (project_id, status, created_at);
CREATE INDEX issues_assigned_agent_idx ON issues (assigned_agent_id) WHERE assigned_agent_id IS NOT NULL;

CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    path text NOT NULL CHECK (btrim(path) <> ''),
    repository_path text,
    base_branch text,
    base_revision text,
    working_branch text NOT NULL CHECK (btrim(working_branch) <> ''),
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
    resource_kind text NOT NULL CHECK (resource_kind IN ('AGENT', 'MODEL_PROFILE')),
    resource_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT scheduler_capacity_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (job_id, resource_kind)
);

CREATE INDEX scheduler_capacity_resource_idx ON scheduler_capacity_reservations (resource_kind, resource_id);
CREATE INDEX scheduler_capacity_run_idx ON scheduler_capacity_reservations (run_id);

CREATE TABLE runtime_instances (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL,
    runtime_id uuid NOT NULL REFERENCES runtimes(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'PROVISIONING' CHECK (status IN ('PROVISIONING', 'STARTING', 'RUNNING', 'STOPPING', 'FAILED', 'STOPPED', 'DESTROYED')),
    external_id text,
    runner_status text NOT NULL DEFAULT 'CONNECTING' CHECK (runner_status IN ('CONNECTING', 'READY', 'BUSY', 'DRAINING', 'UNAVAILABLE')),
    safe_handle_metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(safe_handle_metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    stopped_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT runtime_instances_workspace_fk FOREIGN KEY (project_id, workspace_id) REFERENCES workspaces(project_id, id) ON DELETE RESTRICT,
    UNIQUE (project_id, id)
);

CREATE INDEX runtime_instances_workspace_idx ON runtime_instances (workspace_id, status);
CREATE INDEX runtime_instances_runtime_idx ON runtime_instances (runtime_id, status);
CREATE UNIQUE INDEX runtime_instances_external_id_uq ON runtime_instances (external_id) WHERE external_id IS NOT NULL;

CREATE TABLE execution_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    run_id uuid NOT NULL,
    runtime_instance_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'STARTING', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED')),
    cwd text NOT NULL DEFAULT '/workspace' CHECK (btrim(cwd) <> ''),
    command_argv jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(command_argv) = 'array'),
    exit_code integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT execution_sessions_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    CONSTRAINT execution_sessions_runtime_instance_fk FOREIGN KEY (project_id, runtime_instance_id) REFERENCES runtime_instances(project_id, id) ON DELETE RESTRICT,
    UNIQUE (project_id, id)
);

CREATE INDEX execution_sessions_run_idx ON execution_sessions (run_id, created_at);
CREATE UNIQUE INDEX execution_sessions_one_active_per_instance_uq
    ON execution_sessions (runtime_instance_id)
    WHERE status IN ('PENDING', 'STARTING', 'RUNNING');

CREATE TABLE questions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    run_id uuid NOT NULL,
    prompt text NOT NULL CHECK (btrim(prompt) <> ''),
    kind text NOT NULL DEFAULT 'TEXT' CHECK (kind IN ('TEXT', 'SINGLE_CHOICE', 'MULTI_CHOICE')),
    options jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(options) = 'array'),
    recommendation text,
    blocking boolean NOT NULL DEFAULT true,
    status text NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'ANSWERED', 'CANCELLED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    answered_at timestamptz,
    CONSTRAINT questions_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT questions_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (project_id, id)
);

CREATE INDEX questions_open_idx ON questions (project_id, status, created_at) WHERE status = 'OPEN';
CREATE INDEX questions_run_idx ON questions (run_id, created_at);

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
    requested_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reviews_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT reviews_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    UNIQUE (run_id),
    UNIQUE (project_id, id)
);

CREATE INDEX reviews_pending_idx ON reviews (project_id, requested_at) WHERE status = 'PENDING';

CREATE TABLE run_provenance (
    run_id uuid PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    snapshot jsonb NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT run_provenance_run_scope_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE
);

CREATE TABLE events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    schema_version integer NOT NULL DEFAULT 1 CHECK (schema_version >= 1),
    type text NOT NULL CHECK (btrim(type) <> ''),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id uuid,
    run_id uuid,
    agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    workspace_id uuid,
    runtime_instance_id uuid,
    correlation_id uuid,
    parent_event_id uuid REFERENCES events(id) ON DELETE SET NULL,
    sequence bigint,
    actor jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(actor) = 'object'),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT events_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT events_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    CONSTRAINT events_workspace_fk FOREIGN KEY (project_id, workspace_id) REFERENCES workspaces(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT events_runtime_instance_fk FOREIGN KEY (project_id, runtime_instance_id) REFERENCES runtime_instances(project_id, id) ON DELETE RESTRICT,
    CHECK ((run_id IS NULL AND sequence IS NULL) OR (run_id IS NOT NULL AND sequence IS NOT NULL AND sequence >= 1)),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX events_run_sequence_uq ON events (run_id, sequence) WHERE run_id IS NOT NULL;
CREATE INDEX events_run_timeline_idx ON events (run_id, sequence) WHERE run_id IS NOT NULL;
CREATE INDEX events_project_timeline_idx ON events (project_id, created_at, id);
CREATE INDEX events_correlation_idx ON events (correlation_id) WHERE correlation_id IS NOT NULL;

CREATE TABLE raw_output_chunks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    run_id uuid NOT NULL,
    stream text NOT NULL CHECK (stream IN ('STDOUT', 'STDERR', 'PROTOCOL', 'RUNTIME', 'DIAGNOSTIC')),
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
    IF TG_TABLE_NAME = 'executor_profiles' THEN
        SELECT project_id INTO referenced_project_id FROM model_profiles WHERE id = NEW.model_profile_id;
        IF referenced_project_id IS NOT NULL AND referenced_project_id IS DISTINCT FROM NEW.project_id THEN
            RAISE EXCEPTION 'executor profile cannot reference model profile from another project' USING ERRCODE = '23514';
        END IF;
        SELECT project_id INTO referenced_project_id FROM runtimes WHERE id = NEW.runtime_id;
        IF referenced_project_id IS NOT NULL AND referenced_project_id IS DISTINCT FROM NEW.project_id THEN
            RAISE EXCEPTION 'executor profile cannot reference runtime from another project' USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'agents' THEN
        SELECT project_id INTO referenced_project_id FROM executor_profiles WHERE id = NEW.executor_profile_id;
        IF referenced_project_id IS NOT NULL AND referenced_project_id IS DISTINCT FROM NEW.project_id THEN
            RAISE EXCEPTION 'agent cannot reference executor profile from another project' USING ERRCODE = '23514';
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
    ELSIF TG_TABLE_NAME = 'runtime_instances' THEN
        SELECT project_id INTO referenced_project_id FROM runtimes WHERE id = NEW.runtime_id;
        IF referenced_project_id IS NOT NULL AND referenced_project_id IS DISTINCT FROM NEW.project_id THEN
            RAISE EXCEPTION 'runtime instance cannot reference runtime from another project' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER executor_profiles_scope_check BEFORE INSERT OR UPDATE OF project_id, model_profile_id, runtime_id ON executor_profiles FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();
CREATE TRIGGER agents_scope_check BEFORE INSERT OR UPDATE OF project_id, executor_profile_id ON agents FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();
CREATE TRIGGER issues_scope_check BEFORE INSERT OR UPDATE OF project_id, assigned_agent_id ON issues FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();
CREATE TRIGGER runs_scope_check BEFORE INSERT OR UPDATE OF project_id, agent_id ON runs FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();
CREATE TRIGGER runtime_instances_scope_check BEFORE INSERT OR UPDATE OF project_id, runtime_id ON runtime_instances FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();

CREATE FUNCTION enforce_runtime_instance_binding() RETURNS trigger AS $$
BEGIN
    IF NEW.project_id IS DISTINCT FROM OLD.project_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.runtime_id IS DISTINCT FROM OLD.runtime_id THEN
        RAISE EXCEPTION 'runtime instance project, workspace and runtime bindings are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER runtime_instances_immutable_binding
    BEFORE UPDATE OF project_id, workspace_id, runtime_id ON runtime_instances
    FOR EACH ROW EXECUTE FUNCTION enforce_runtime_instance_binding();

CREATE FUNCTION enforce_execution_session_workspace() RETURNS trigger AS $$
DECLARE
    run_workspace_id uuid;
    instance_workspace_id uuid;
BEGIN
    SELECT workspace_id INTO run_workspace_id FROM runs WHERE project_id = NEW.project_id AND id = NEW.run_id;
    SELECT workspace_id INTO instance_workspace_id FROM runtime_instances WHERE project_id = NEW.project_id AND id = NEW.runtime_instance_id;
    IF run_workspace_id IS NULL OR instance_workspace_id IS NULL OR run_workspace_id IS DISTINCT FROM instance_workspace_id THEN
        RAISE EXCEPTION 'execution session Run and Runtime Instance must use the same Workspace' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER execution_sessions_workspace_check
    BEFORE INSERT OR UPDATE OF project_id, run_id, runtime_instance_id ON execution_sessions
    FOR EACH ROW EXECUTE FUNCTION enforce_execution_session_workspace();

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