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

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL CHECK (btrim(username) <> '' AND username = lower(btrim(username))),
    email text NOT NULL CHECK (btrim(email) <> '' AND email = lower(btrim(email))),
    display_name text NOT NULL CHECK (btrim(display_name) <> ''),
    password_hash text CHECK (password_hash IS NULL OR btrim(password_hash) <> ''),
    deployment_role text NOT NULL CHECK (deployment_role IN ('admin', 'member')),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'disabled')),
    force_password_change boolean NOT NULL DEFAULT false,
    auth_version bigint NOT NULL DEFAULT 1 CHECK (auth_version >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (status <> 'active' OR password_hash IS NOT NULL)
);

CREATE UNIQUE INDEX users_username_uq ON users (username);
CREATE UNIQUE INDEX users_email_uq ON users (email);

CREATE TABLE groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (btrim(name) <> '' AND name = lower(btrim(name))),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX groups_name_uq ON groups (name);

CREATE TABLE group_members (
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE project_user_access (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    role text NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX project_user_access_user_idx ON project_user_access (user_id, project_id);

CREATE TABLE project_group_access (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    PRIMARY KEY (project_id, group_id)
);

CREATE INDEX project_group_access_group_idx ON project_group_access (group_id, project_id);

CREATE TABLE auth_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    access_token_lifetime_seconds integer NOT NULL DEFAULT 3600 CHECK (access_token_lifetime_seconds BETWEEN 300 AND 86400),
    refresh_token_lifetime_seconds integer NOT NULL DEFAULT 2592000 CHECK (refresh_token_lifetime_seconds BETWEEN 3600 AND 31536000),
    password_minimum_length integer NOT NULL DEFAULT 12 CHECK (password_minimum_length BETWEEN 8 AND 256),
    require_uppercase boolean NOT NULL DEFAULT false,
    require_lowercase boolean NOT NULL DEFAULT false,
    require_number boolean NOT NULL DEFAULT false,
    require_symbol boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO auth_settings (singleton) VALUES (true);

CREATE TABLE auth_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash bytea NOT NULL CHECK (octet_length(refresh_token_hash) = 32),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK (last_used_at IS NULL OR last_used_at >= created_at)
);

CREATE UNIQUE INDEX auth_sessions_refresh_token_uq ON auth_sessions (refresh_token_hash);
CREATE INDEX auth_sessions_user_idx ON auth_sessions (user_id, created_at DESC);

CREATE TABLE auth_session_refresh_tokens (
    session_id uuid NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32)
);

CREATE TABLE password_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('setup', 'reset')),
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK (consumed_at IS NULL OR revoked_at IS NULL)
);

CREATE UNIQUE INDEX password_tokens_hash_uq ON password_tokens (token_hash);
CREATE UNIQUE INDEX password_tokens_active_user_purpose_uq
    ON password_tokens (user_id, purpose)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE TABLE runners (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects(id) ON DELETE RESTRICT,
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
    CHECK (NOT internal OR project_id IS NULL),
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
CREATE INDEX runners_project_idx ON runners (project_id, created_at, id) WHERE project_id IS NOT NULL;

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
    filtered_model_count integer CHECK (filtered_model_count IS NULL OR filtered_model_count >= 0),
    total_model_count integer CHECK (total_model_count IS NULL OR total_model_count >= 0),
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
CREATE TABLE agents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    role_instructions text NOT NULL DEFAULT '',
    engine text NOT NULL CHECK (btrim(engine) <> ''),
    model_profile_id uuid NOT NULL REFERENCES model_profiles(id) ON DELETE RESTRICT,
    engine_settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(engine_settings) = 'object'),
    concurrency_limit integer NOT NULL DEFAULT 1 CHECK (concurrency_limit >= 1),
    allow_delegation boolean NOT NULL DEFAULT false,
    state text NOT NULL DEFAULT 'ENABLED' CHECK (state IN ('DRAFT', 'ENABLED', 'DISABLED', 'ARCHIVED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX agents_global_name_uq ON agents (lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX agents_project_name_uq ON agents (project_id, lower(name)) WHERE project_id IS NOT NULL;
CREATE INDEX agents_model_profile_idx ON agents (model_profile_id);

CREATE TABLE squads (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    leader_agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX squads_project_name_uq ON squads (project_id, lower(name));
CREATE INDEX squads_leader_idx ON squads (leader_agent_id);

CREATE TABLE squad_members (
    squad_id uuid NOT NULL REFERENCES squads(id) ON DELETE CASCADE,
    agent_id uuid REFERENCES agents(id) ON DELETE RESTRICT,
    user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    role text CHECK (role IS NULL OR btrim(role) <> ''),
    CONSTRAINT squad_members_typed_identity CHECK (
        (agent_id IS NOT NULL AND user_id IS NULL)
        OR (agent_id IS NULL AND user_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX squad_members_agent_uq ON squad_members (squad_id, agent_id) WHERE agent_id IS NOT NULL;
CREATE UNIQUE INDEX squad_members_user_uq ON squad_members (squad_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX squad_members_agent_idx ON squad_members (agent_id, squad_id) WHERE agent_id IS NOT NULL;
CREATE INDEX squad_members_user_idx ON squad_members (user_id, squad_id) WHERE user_id IS NOT NULL;

CREATE TABLE issues (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number integer NOT NULL CHECK (number >= 1),
    title text NOT NULL CHECK (btrim(title) <> ''),
    description text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'BACKLOG' CHECK (status IN ('BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE')),
    priority integer NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
    board_position bigint NOT NULL DEFAULT 0 CHECK (board_position >= 0),
    assignee_type text,
    assignee_id uuid,
    CONSTRAINT issues_assignee_pair CHECK ((assignee_type IS NULL AND assignee_id IS NULL) OR (assignee_type IS NOT NULL AND assignee_type IN ('USER','AGENT','SQUAD') AND assignee_id IS NOT NULL)),
    created_by_type text CHECK (created_by_type IS NULL OR created_by_type IN ('HUMAN', 'AGENT')),
    created_by_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, id),
    UNIQUE (project_id, number),
    CHECK ((created_by_type IS NULL) = (created_by_id IS NULL))
);

CREATE INDEX issues_project_status_idx ON issues (project_id, status, board_position, created_at, id);
CREATE INDEX issues_assignee_idx ON issues (assignee_type, assignee_id) WHERE assignee_id IS NOT NULL;

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

CREATE TABLE issue_comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    parent_comment_id uuid,
    author_type text NOT NULL CHECK (author_type IN ('HUMAN', 'AGENT')),
    author_id uuid NOT NULL,
    source_run_id uuid,
    source_action_key text,
    body text,
    suppress_implicit_agent_trigger boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    resolved_at timestamptz,
    resolved_by_user_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issue_id, id),
    CONSTRAINT issue_comments_parent_fk FOREIGN KEY (issue_id, parent_comment_id)
        REFERENCES issue_comments(issue_id, id),
    CHECK (parent_comment_id IS NULL OR parent_comment_id <> id),
    CHECK (
        (deleted_at IS NULL AND body IS NOT NULL AND btrim(body) <> '')
        OR (deleted_at IS NOT NULL AND body IS NULL)
    ),
    CHECK ((resolved_at IS NULL) = (resolved_by_user_id IS NULL)),
    CHECK (parent_comment_id IS NULL OR (resolved_at IS NULL AND resolved_by_user_id IS NULL)),
    CHECK (author_type = 'HUMAN' OR NOT suppress_implicit_agent_trigger),
    CHECK (
        (author_type = 'HUMAN' AND source_run_id IS NULL AND (source_action_key IS NULL OR btrim(source_action_key) <> ''))
        OR (author_type = 'AGENT' AND source_run_id IS NOT NULL AND source_action_key IS NOT NULL AND btrim(source_action_key) <> '')
    )
);

CREATE INDEX issue_comments_issue_timeline_idx ON issue_comments (issue_id, created_at, id);
CREATE INDEX issue_comments_parent_idx ON issue_comments (issue_id, parent_comment_id, created_at, id);
CREATE UNIQUE INDEX issue_comments_source_action_uq
    ON issue_comments (source_run_id, source_action_key)
    WHERE source_run_id IS NOT NULL AND source_action_key IS NOT NULL;
CREATE UNIQUE INDEX issue_comments_human_action_uq
    ON issue_comments (issue_id, author_id, source_action_key)
    WHERE author_type = 'HUMAN' AND source_action_key IS NOT NULL;

CREATE TABLE issue_subscriptions (
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, user_id),
    CONSTRAINT issue_subscriptions_issue_fk FOREIGN KEY (project_id, issue_id)
        REFERENCES issues(project_id, id) ON DELETE CASCADE
);

CREATE INDEX issue_subscriptions_user_idx ON issue_subscriptions (user_id, created_at DESC, issue_id);

CREATE TABLE user_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    source_comment_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('COMMENT_REPLY', 'ISSUE_COMMENT')),
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at timestamptz,
    CONSTRAINT user_notifications_issue_fk FOREIGN KEY (project_id, issue_id)
        REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT user_notifications_comment_fk FOREIGN KEY (issue_id, source_comment_id)
        REFERENCES issue_comments(issue_id, id) ON DELETE CASCADE,
    UNIQUE (recipient_user_id, source_comment_id)
);

CREATE INDEX user_notifications_recipient_idx
    ON user_notifications (recipient_user_id, created_at DESC, id DESC);
CREATE INDEX user_notifications_unread_idx
    ON user_notifications (recipient_user_id, created_at DESC, id DESC)
    WHERE read_at IS NULL;

CREATE TABLE issue_comment_reactions (
    issue_id uuid NOT NULL,
    comment_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    reaction text NOT NULL CHECK (reaction IN ('THUMBS_UP', 'THUMBS_DOWN', 'LAUGH', 'HOORAY', 'CONFUSED', 'HEART', 'ROCKET', 'EYES')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_comment_reactions_comment_fk FOREIGN KEY (issue_id, comment_id)
        REFERENCES issue_comments(issue_id, id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, comment_id, actor_id, reaction)
);

CREATE INDEX issue_comment_reactions_comment_idx
    ON issue_comment_reactions (issue_id, comment_id, reaction, created_at, actor_id);

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
    UNIQUE (project_id, id),
    UNIQUE (project_id, issue_id, id)
);

CREATE INDEX runs_project_status_idx ON runs (project_id, status, created_at);
CREATE INDEX runs_issue_created_idx ON runs (issue_id, created_at DESC);
CREATE INDEX runs_agent_active_idx ON runs (agent_id, status) WHERE agent_id IS NOT NULL AND status IN ('STARTING', 'RUNNING');

ALTER TABLE issue_comments
    ADD CONSTRAINT issue_comments_source_run_fk
    FOREIGN KEY (source_run_id) REFERENCES runs(id) ON DELETE RESTRICT;

CREATE TABLE delegations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    parent_run_id uuid,
    parent_agent_id uuid REFERENCES agents(id) ON DELETE RESTRICT,
    source_comment_id uuid,
    target_agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    task text NOT NULL CHECK (btrim(task) <> ''),
    delegated_run_id uuid NOT NULL,
    request_key text NOT NULL CHECK (btrim(request_key) <> ''),
    outcome text CHECK (outcome IN ('SUCCEEDED', 'FAILED', 'CANCELLED')),
    result_summary text CHECK (result_summary IS NULL OR char_length(result_summary) <= 4096),
    result_event_id uuid,
    workspace_changes_accepted boolean,
    continuation_job_id uuid,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT delegations_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT delegations_parent_run_fk FOREIGN KEY (project_id, issue_id, parent_run_id) REFERENCES runs(project_id, issue_id, id) ON DELETE CASCADE,
    CONSTRAINT delegations_source_comment_fk FOREIGN KEY (issue_id, source_comment_id) REFERENCES issue_comments(issue_id, id) ON DELETE RESTRICT,
    CONSTRAINT delegations_delegated_run_fk FOREIGN KEY (project_id, issue_id, delegated_run_id) REFERENCES runs(project_id, issue_id, id) ON DELETE CASCADE,
    CHECK (
        (parent_run_id IS NOT NULL AND parent_agent_id IS NOT NULL AND source_comment_id IS NULL)
        OR (parent_run_id IS NULL AND parent_agent_id IS NULL AND source_comment_id IS NOT NULL)
    ),
    CHECK (parent_agent_id IS NULL OR parent_agent_id <> target_agent_id),
    CHECK ((outcome IS NULL) = (completed_at IS NULL)),
    CHECK (outcome IS NULL OR (result_summary IS NOT NULL AND workspace_changes_accepted IS NOT NULL)),
    UNIQUE (delegated_run_id),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX delegations_parent_request_uq
    ON delegations (parent_run_id, request_key)
    WHERE parent_run_id IS NOT NULL;
CREATE UNIQUE INDEX delegations_comment_request_uq
    ON delegations (source_comment_id, request_key)
    WHERE source_comment_id IS NOT NULL;
CREATE INDEX delegations_parent_run_idx ON delegations (project_id, parent_run_id, created_at, id) WHERE parent_run_id IS NOT NULL;
CREATE INDEX delegations_source_comment_idx ON delegations (project_id, source_comment_id, created_at, id) WHERE source_comment_id IS NOT NULL;
CREATE INDEX delegations_target_agent_idx ON delegations (project_id, target_agent_id, created_at, id);

CREATE TABLE agent_work_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    target_agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    authority_kind text NOT NULL CHECK (authority_kind IN ('ISSUE', 'PARENT_RUN')),
    parent_run_id uuid,
    run_id uuid,
    delegation_id uuid,
    sealed_at timestamptz,
    closed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agent_work_requests_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT agent_work_requests_workspace_fk FOREIGN KEY (project_id, issue_id, workspace_id) REFERENCES workspaces(project_id, issue_id, id) ON DELETE RESTRICT,
    CONSTRAINT agent_work_requests_parent_run_fk FOREIGN KEY (project_id, issue_id, parent_run_id) REFERENCES runs(project_id, issue_id, id) ON DELETE CASCADE,
    CONSTRAINT agent_work_requests_run_fk FOREIGN KEY (project_id, issue_id, run_id) REFERENCES runs(project_id, issue_id, id) ON DELETE CASCADE,
    CONSTRAINT agent_work_requests_delegation_fk FOREIGN KEY (project_id, delegation_id) REFERENCES delegations(project_id, id) ON DELETE RESTRICT,
    CHECK (
        (authority_kind = 'ISSUE' AND parent_run_id IS NULL)
        OR (authority_kind = 'PARENT_RUN' AND parent_run_id IS NOT NULL)
    ),
    CHECK (closed_at IS NULL OR sealed_at IS NOT NULL),
    CHECK (delegation_id IS NULL OR run_id IS NOT NULL),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX agent_work_requests_open_issue_uq
    ON agent_work_requests (project_id, issue_id, workspace_id, target_agent_id)
    WHERE authority_kind = 'ISSUE' AND sealed_at IS NULL;
CREATE UNIQUE INDEX agent_work_requests_open_parent_uq
    ON agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, parent_run_id)
    WHERE authority_kind = 'PARENT_RUN' AND sealed_at IS NULL;
CREATE INDEX agent_work_requests_pending_idx
    ON agent_work_requests (updated_at, created_at, id)
    WHERE delegation_id IS NULL AND sealed_at IS NULL;
CREATE UNIQUE INDEX agent_work_requests_open_run_uq
    ON agent_work_requests (project_id, run_id)
    WHERE run_id IS NOT NULL AND closed_at IS NULL;
CREATE INDEX agent_work_requests_run_idx
    ON agent_work_requests (project_id, run_id) WHERE run_id IS NOT NULL;
CREATE INDEX agent_work_requests_delegation_idx
    ON agent_work_requests (project_id, delegation_id) WHERE delegation_id IS NOT NULL;

CREATE TABLE issue_comment_mentions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    comment_id uuid NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    target_agent_id uuid NOT NULL,
    target_type text NOT NULL DEFAULT 'AGENT' CHECK (target_type IN ('AGENT', 'SQUAD')),
    target_id uuid NOT NULL,
    resolved_agent_id uuid,
    outcome text NOT NULL CHECK (outcome IN ('QUEUED', 'COALESCED', 'DEFERRED', 'BLOCKED')),
    reason_code text CHECK (reason_code IS NULL OR btrim(reason_code) <> ''),
    delegation_id uuid,
    work_request_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_comment_mentions_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_comment_mentions_comment_fk FOREIGN KEY (issue_id, comment_id) REFERENCES issue_comments(issue_id, id) ON DELETE RESTRICT,
    CONSTRAINT issue_comment_mentions_delegation_fk FOREIGN KEY (project_id, delegation_id) REFERENCES delegations(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT issue_comment_mentions_work_request_fk FOREIGN KEY (project_id, work_request_id) REFERENCES agent_work_requests(project_id, id) ON DELETE RESTRICT,
    CHECK (
        (outcome = 'QUEUED' AND work_request_id IS NOT NULL AND delegation_id IS NOT NULL AND reason_code IS NULL)
        OR (outcome IN ('COALESCED', 'DEFERRED') AND work_request_id IS NOT NULL AND delegation_id IS NULL AND reason_code IS NULL)
        OR (outcome = 'BLOCKED' AND work_request_id IS NULL AND delegation_id IS NULL AND reason_code IS NOT NULL)
    ),
    UNIQUE (issue_id, comment_id, target_type, target_id),
    UNIQUE (issue_id, comment_id, ordinal),
    UNIQUE (project_id, id)
);

CREATE INDEX issue_comment_mentions_comment_idx ON issue_comment_mentions (project_id, issue_id, comment_id, ordinal);
CREATE INDEX issue_comment_mentions_delegation_idx ON issue_comment_mentions (project_id, delegation_id) WHERE delegation_id IS NOT NULL;
CREATE INDEX issue_comment_mentions_work_request_idx ON issue_comment_mentions (project_id, work_request_id) WHERE work_request_id IS NOT NULL;

CREATE TABLE issue_comment_implicit_triggers (
    project_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    comment_id uuid NOT NULL,
    target_agent_id uuid NOT NULL,
    target_type text NOT NULL DEFAULT 'AGENT' CHECK (target_type IN ('AGENT', 'SQUAD')),
    target_id uuid NOT NULL,
    resolved_agent_id uuid,
    routing_reason text NOT NULL CHECK (routing_reason IN ('DIRECT_AGENT_REPLY', 'UNIQUE_THREAD_AGENT', 'ISSUE_ASSIGNEE', 'ISSUE_SQUAD_ASSIGNEE')),
    outcome text NOT NULL CHECK (outcome IN ('QUEUED', 'COALESCED', 'DEFERRED', 'BLOCKED', 'SUPPRESSED')),
    reason_code text CHECK (reason_code IS NULL OR btrim(reason_code) <> ''),
    delegation_id uuid,
    work_request_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_comment_implicit_triggers_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_comment_implicit_triggers_comment_fk FOREIGN KEY (issue_id, comment_id) REFERENCES issue_comments(issue_id, id) ON DELETE RESTRICT,
    CONSTRAINT issue_comment_implicit_triggers_delegation_fk FOREIGN KEY (project_id, delegation_id) REFERENCES delegations(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT issue_comment_implicit_triggers_work_request_fk FOREIGN KEY (project_id, work_request_id) REFERENCES agent_work_requests(project_id, id) ON DELETE RESTRICT,
    CHECK (
        (outcome = 'QUEUED' AND work_request_id IS NOT NULL AND delegation_id IS NOT NULL AND reason_code IS NULL)
        OR (outcome IN ('COALESCED', 'DEFERRED') AND work_request_id IS NOT NULL AND delegation_id IS NULL AND reason_code IS NULL)
        OR (outcome = 'BLOCKED' AND work_request_id IS NULL AND delegation_id IS NULL AND reason_code IS NOT NULL)
        OR (outcome = 'SUPPRESSED' AND work_request_id IS NULL AND delegation_id IS NULL AND reason_code IS NULL)
    ),
    PRIMARY KEY (issue_id, comment_id),
    UNIQUE (project_id, issue_id, comment_id)
);

CREATE INDEX issue_comment_implicit_triggers_project_idx
    ON issue_comment_implicit_triggers (project_id, issue_id, comment_id);
CREATE INDEX issue_comment_implicit_triggers_delegation_idx
    ON issue_comment_implicit_triggers (project_id, delegation_id) WHERE delegation_id IS NOT NULL;
CREATE INDEX issue_comment_implicit_triggers_work_request_idx
    ON issue_comment_implicit_triggers (project_id, work_request_id) WHERE work_request_id IS NOT NULL;

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

CREATE TABLE runtime_instances (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL,
    runtime_id uuid NOT NULL REFERENCES runtimes(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'PROVISIONING' CHECK (status IN ('PROVISIONING', 'STARTING', 'RUNNING', 'STOPPING', 'FAILED', 'STOPPED', 'DESTROYED')),
    external_id text,
    runner_status text NOT NULL DEFAULT 'CONNECTING' CHECK (runner_status IN ('CONNECTING', 'READY', 'BUSY', 'DRAINING', 'UNAVAILABLE')),
    runner_generation bigint NOT NULL DEFAULT 0 CHECK (runner_generation >= 0),
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
    runtime_instance_id uuid,
    runner_id uuid REFERENCES runners(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'STARTING', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED')),
    cwd text NOT NULL DEFAULT '/workspace' CHECK (btrim(cwd) <> ''),
    command_argv jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(command_argv) = 'array'),
    admission_prompt text CHECK (admission_prompt IS NULL OR btrim(admission_prompt) <> ''),
    exit_code integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT execution_sessions_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE CASCADE,
    CONSTRAINT execution_sessions_runtime_instance_fk FOREIGN KEY (project_id, runtime_instance_id) REFERENCES runtime_instances(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT execution_sessions_owner_check CHECK (
        (runner_id IS NOT NULL AND runtime_instance_id IS NULL) OR
        (runner_id IS NULL AND runtime_instance_id IS NOT NULL)
    ),
    UNIQUE (project_id, id)
);

CREATE INDEX execution_sessions_run_idx ON execution_sessions (run_id, created_at);
CREATE INDEX execution_sessions_runner_idx ON execution_sessions (runner_id, created_at) WHERE runner_id IS NOT NULL;
CREATE UNIQUE INDEX execution_sessions_one_active_per_instance_uq
    ON execution_sessions (runtime_instance_id)
    WHERE status IN ('PENDING', 'STARTING', 'RUNNING') AND runtime_instance_id IS NOT NULL;

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
    runtime_instance_id uuid,
    correlation_id uuid,
    parent_event_id uuid REFERENCES events(id) ON DELETE RESTRICT,
    sequence bigint,
    actor jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(actor) = 'object'),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT events_issue_fk FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT events_run_fk FOREIGN KEY (project_id, run_id) REFERENCES runs(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT events_workspace_fk FOREIGN KEY (project_id, workspace_id) REFERENCES workspaces(project_id, id) ON DELETE RESTRICT,
    CONSTRAINT events_runtime_instance_fk FOREIGN KEY (project_id, runtime_instance_id) REFERENCES runtime_instances(project_id, id) ON DELETE RESTRICT,
    CHECK ((run_id IS NULL AND sequence IS NULL) OR (run_id IS NOT NULL AND sequence IS NOT NULL AND sequence >= 1)),
    UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX events_run_sequence_uq ON events (run_id, sequence) WHERE run_id IS NOT NULL;
CREATE INDEX events_run_timeline_idx ON events (run_id, sequence) WHERE run_id IS NOT NULL;
CREATE INDEX events_project_timeline_idx ON events (project_id, created_at, id);
CREATE INDEX events_issue_timeline_idx ON events (project_id, issue_id, occurred_at, id) WHERE issue_id IS NOT NULL;
CREATE INDEX events_correlation_idx ON events (correlation_id) WHERE correlation_id IS NOT NULL;
CREATE INDEX events_question_id_lookup_idx ON events (project_id, run_id, type, (payload->>'questionId'));

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

-- Usernames and email addresses share one normalized deployment-wide login
-- namespace. Per-column unique indexes cannot protect cross-field collisions, so
-- serialize only writes that touch the same identifiers and reject the collision
-- at the database boundary as a uniqueness violation.
CREATE FUNCTION enforce_user_login_namespace() RETURNS trigger AS $$
DECLARE
    first_identifier text := LEAST(NEW.username, NEW.email);
    second_identifier text := GREATEST(NEW.username, NEW.email);
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('agent-board:user-login:' || first_identifier, 0));
    IF second_identifier <> first_identifier THEN
        PERFORM pg_advisory_xact_lock(hashtextextended('agent-board:user-login:' || second_identifier, 0));
    END IF;

    IF EXISTS (
        SELECT 1
        FROM users
        WHERE id IS DISTINCT FROM NEW.id
          AND (
              username IN (NEW.username, NEW.email)
              OR email IN (NEW.username, NEW.email)
          )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23505',
            MESSAGE = 'user login identifier already exists',
            CONSTRAINT = 'users_login_namespace_uq';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_login_namespace_check
    BEFORE INSERT OR UPDATE OF username, email ON users
    FOR EACH ROW EXECUTE FUNCTION enforce_user_login_namespace();

-- Squad leadership is stored once on squads. Agent member scope is enforced here;
-- User member workflow eligibility is dynamic Project access and is validated by
-- the shared application/store write path instead of being persisted as an ACL.
CREATE FUNCTION enforce_squad_agent_scope() RETURNS trigger AS $$
DECLARE
    squad_project_id uuid;
    squad_leader_agent_id uuid;
    agent_project_id uuid;
BEGIN
    IF TG_TABLE_NAME = 'squads' THEN
        IF TG_OP = 'UPDATE' AND NEW.project_id IS DISTINCT FROM OLD.project_id THEN
            RAISE EXCEPTION 'Squad Project is immutable' USING ERRCODE = '55000';
        END IF;
        SELECT project_id INTO agent_project_id
        FROM agents
        WHERE id = NEW.leader_agent_id
        FOR UPDATE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'invalid Squad leader Agent' USING ERRCODE = '23514';
        END IF;
        IF agent_project_id IS NOT NULL AND agent_project_id IS DISTINCT FROM NEW.project_id THEN
            RAISE EXCEPTION 'Squad leader Agent is outside Project scope' USING ERRCODE = '23514';
        END IF;
        IF EXISTS (
            SELECT 1 FROM squad_members
            WHERE squad_id = NEW.id AND agent_id = NEW.leader_agent_id
        ) THEN
            RAISE EXCEPTION 'Squad leader cannot also be an additional member' USING ERRCODE = '23514';
        END IF;
    ELSE
        SELECT project_id, leader_agent_id
        INTO squad_project_id, squad_leader_agent_id
        FROM squads
        WHERE id = NEW.squad_id
        FOR UPDATE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'invalid Squad membership' USING ERRCODE = '23514';
        END IF;
        IF NEW.agent_id IS NULL THEN
            RETURN NEW;
        END IF;
        IF NEW.agent_id = squad_leader_agent_id THEN
            RAISE EXCEPTION 'Squad leader cannot also be an additional member' USING ERRCODE = '23514';
        END IF;
        SELECT project_id INTO agent_project_id
        FROM agents
        WHERE id = NEW.agent_id
        FOR UPDATE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'invalid Squad member Agent' USING ERRCODE = '23514';
        END IF;
        IF agent_project_id IS NOT NULL AND agent_project_id IS DISTINCT FROM squad_project_id THEN
            RAISE EXCEPTION 'Squad member Agent is outside Project scope' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER squads_agent_scope_check
    BEFORE INSERT OR UPDATE OF project_id, leader_agent_id ON squads
    FOR EACH ROW EXECUTE FUNCTION enforce_squad_agent_scope();
CREATE TRIGGER squad_members_agent_scope_check
    BEFORE INSERT OR UPDATE OF squad_id, agent_id, user_id ON squad_members
    FOR EACH ROW EXECUTE FUNCTION enforce_squad_agent_scope();

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
        IF NEW.assignee_type = 'AGENT' THEN
            SELECT project_id INTO referenced_project_id FROM agents WHERE id = NEW.assignee_id;
            IF NOT FOUND THEN RAISE EXCEPTION 'invalid agent assignee' USING ERRCODE = '23514'; END IF;
            IF referenced_project_id IS NOT NULL AND referenced_project_id IS DISTINCT FROM NEW.project_id THEN
                RAISE EXCEPTION 'issue cannot reference agent from another project' USING ERRCODE = '23514';
            END IF;
        ELSIF NEW.assignee_type = 'USER' THEN
            PERFORM 1 FROM users WHERE id = NEW.assignee_id;
            IF NOT FOUND THEN RAISE EXCEPTION 'invalid user assignee' USING ERRCODE = '23514'; END IF;
        ELSIF NEW.assignee_type = 'SQUAD' THEN
            SELECT project_id INTO referenced_project_id FROM squads WHERE id = NEW.assignee_id;
            IF NOT FOUND THEN RAISE EXCEPTION 'invalid Squad assignee' USING ERRCODE = '23514'; END IF;
            IF referenced_project_id IS DISTINCT FROM NEW.project_id THEN
                RAISE EXCEPTION 'issue cannot reference Squad from another project' USING ERRCODE = '23514';
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
    ELSIF TG_TABLE_NAME = 'runtimes' THEN
        IF EXISTS (
            SELECT 1 FROM runtime_instances
            WHERE runtime_id = NEW.id AND project_id IS DISTINCT FROM NEW.project_id
        ) THEN
            RAISE EXCEPTION 'runtime ownership change would cross project scope' USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'agents' THEN
        IF EXISTS (
            SELECT 1 FROM squads
            WHERE leader_agent_id = NEW.id AND project_id IS DISTINCT FROM NEW.project_id
        ) OR EXISTS (
            SELECT 1
            FROM squad_members sm
            JOIN squads s ON s.id = sm.squad_id
            WHERE sm.agent_id = NEW.id AND s.project_id IS DISTINCT FROM NEW.project_id
        ) THEN
            RAISE EXCEPTION 'agent ownership change would cross Squad project scope' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER model_profiles_owner_change_check BEFORE UPDATE OF project_id ON model_profiles FOR EACH ROW EXECUTE FUNCTION enforce_configuration_owner_change();
CREATE TRIGGER runtimes_owner_change_check BEFORE UPDATE OF project_id ON runtimes FOR EACH ROW EXECUTE FUNCTION enforce_configuration_owner_change();
CREATE TRIGGER agents_owner_change_check BEFORE UPDATE OF project_id ON agents FOR EACH ROW EXECUTE FUNCTION enforce_configuration_owner_change();

CREATE TRIGGER agents_scope_check BEFORE INSERT OR UPDATE OF project_id, model_profile_id ON agents FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();
CREATE TRIGGER issues_scope_check BEFORE INSERT OR UPDATE OF project_id, assignee_type, assignee_id ON issues FOR EACH ROW EXECUTE FUNCTION enforce_configuration_scope();
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
    IF NEW.runtime_instance_id IS NULL THEN
        RETURN NEW;
    END IF;
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

CREATE FUNCTION enforce_execution_session_runner_scope() RETURNS trigger AS $$
DECLARE
    runner_project_id uuid;
BEGIN
    IF NEW.runner_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id INTO runner_project_id FROM runners WHERE id = NEW.runner_id;
    IF runner_project_id IS NOT NULL AND runner_project_id IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'execution session cannot use runner owned by another project' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER execution_sessions_runner_scope_check
    BEFORE INSERT OR UPDATE OF project_id, runner_id ON execution_sessions
    FOR EACH ROW EXECUTE FUNCTION enforce_execution_session_runner_scope();

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
