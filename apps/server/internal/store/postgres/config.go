package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateProvider(ctx context.Context, input store.Provider) (store.Provider, error) {
	health := input.HealthStatus
	if health == "" {
		health = "UNKNOWN"
	}
	return scanProvider(s.pool.QueryRow(ctx, `
		INSERT INTO providers (name, kind, base_url, credential_ref, enabled, health_status, safe_metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text, name, kind, base_url, credential_ref, enabled, health_status, safe_metadata, created_at, updated_at
	`, input.Name, input.Kind, input.BaseURL, input.CredentialRef, input.Enabled, health, objectJSON(input.SafeMetadata)))
}

func (s *Store) CreateModelProfile(ctx context.Context, input store.ModelProfile) (store.ModelProfile, error) {
	return scanModelProfile(s.pool.QueryRow(ctx, `
		INSERT INTO model_profiles (project_id, provider_id, name, model, temperature, max_tokens, max_concurrent, generation_settings, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, project_id::text, provider_id::text, name, model, temperature, max_tokens, max_concurrent, generation_settings, enabled, created_at, updated_at
	`, input.ProjectID, input.ProviderID, input.Name, input.Model, input.Temperature, input.MaxTokens, input.MaxConcurrent, objectJSON(input.GenerationSettings), input.Enabled))
}

func (s *Store) CreateRuntime(ctx context.Context, input store.Runtime) (store.Runtime, error) {
	workspacePolicy := input.WorkspacePolicy
	if workspacePolicy == "" {
		workspacePolicy = "issue"
	}
	health := input.HealthStatus
	if health == "" {
		health = "UNKNOWN"
	}
	allowedSecretRefs := input.AllowedSecretRefs
	if allowedSecretRefs == nil {
		allowedSecretRefs = []string{}
	}
	return scanRuntime(s.pool.QueryRow(ctx, `
		INSERT INTO runtimes (project_id, name, kind, image, cpu_limit_millis, memory_limit_bytes, pid_limit, timeout_seconds, network_policy, workspace_policy, allowed_secret_refs, capabilities, enabled, health_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id::text, project_id::text, name, kind, image, cpu_limit_millis, memory_limit_bytes, pid_limit, timeout_seconds, network_policy, workspace_policy, allowed_secret_refs, capabilities, enabled, health_status, created_at, updated_at
	`, input.ProjectID, input.Name, input.Kind, input.Image, input.CPULimitMillis, input.MemoryLimitBytes, input.PIDLimit, input.TimeoutSeconds, input.NetworkPolicy, workspacePolicy, allowedSecretRefs, objectJSON(input.Capabilities), input.Enabled, health))
}

func (s *Store) CreateExecutorProfile(ctx context.Context, input store.ExecutorProfile) (store.ExecutorProfile, error) {
	return scanExecutorProfile(s.pool.QueryRow(ctx, `
		INSERT INTO executor_profiles (project_id, name, engine, model_profile_id, runtime_id, engine_settings, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text, project_id::text, name, engine, model_profile_id::text, runtime_id::text, engine_settings, enabled, created_at, updated_at
	`, input.ProjectID, input.Name, input.Engine, input.ModelProfileID, input.RuntimeID, objectJSON(input.EngineSettings), input.Enabled))
}

func (s *Store) CreateAgent(ctx context.Context, input store.Agent) (store.Agent, error) {
	limit := input.ConcurrencyLimit
	if limit == 0 {
		limit = 1
	}
	state := input.State
	if state == "" {
		state = "ENABLED"
	}
	return scanAgent(s.pool.QueryRow(ctx, `
		INSERT INTO agents (project_id, name, role_instructions, executor_profile_id, concurrency_limit, state)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text, project_id::text, name, role_instructions, executor_profile_id::text, concurrency_limit, state, created_at, updated_at
	`, input.ProjectID, input.Name, input.RoleInstructions, input.ExecutorProfileID, limit, state))
}

func (s *Store) GetAgent(ctx context.Context, projectID, agentID string) (store.Agent, error) {
	return scanAgent(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, name, role_instructions, executor_profile_id::text, concurrency_limit, state, created_at, updated_at
		FROM agents
		WHERE id = $2 AND (project_id IS NULL OR project_id = $1)
	`, projectID, agentID))
}

func scanProvider(row pgx.Row) (store.Provider, error) {
	var value store.Provider
	if err := row.Scan(&value.ID, &value.Name, &value.Kind, &value.BaseURL, &value.CredentialRef, &value.Enabled, &value.HealthStatus, &value.SafeMetadata, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Provider{}, notFound(err)
	}
	return value, nil
}

func scanModelProfile(row pgx.Row) (store.ModelProfile, error) {
	var value store.ModelProfile
	if err := row.Scan(&value.ID, &value.ProjectID, &value.ProviderID, &value.Name, &value.Model, &value.Temperature, &value.MaxTokens, &value.MaxConcurrent, &value.GenerationSettings, &value.Enabled, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.ModelProfile{}, notFound(err)
	}
	return value, nil
}

func scanRuntime(row pgx.Row) (store.Runtime, error) {
	var value store.Runtime
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Name, &value.Kind, &value.Image, &value.CPULimitMillis, &value.MemoryLimitBytes, &value.PIDLimit, &value.TimeoutSeconds, &value.NetworkPolicy, &value.WorkspacePolicy, &value.AllowedSecretRefs, &value.Capabilities, &value.Enabled, &value.HealthStatus, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Runtime{}, notFound(err)
	}
	return value, nil
}

func scanExecutorProfile(row pgx.Row) (store.ExecutorProfile, error) {
	var value store.ExecutorProfile
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Name, &value.Engine, &value.ModelProfileID, &value.RuntimeID, &value.EngineSettings, &value.Enabled, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.ExecutorProfile{}, notFound(err)
	}
	return value, nil
}

func scanAgent(row pgx.Row) (store.Agent, error) {
	var value store.Agent
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Name, &value.RoleInstructions, &value.ExecutorProfileID, &value.ConcurrencyLimit, &value.State, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Agent{}, notFound(err)
	}
	return value, nil
}
