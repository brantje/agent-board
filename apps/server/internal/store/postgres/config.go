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
		INSERT INTO providers (project_id, name, kind, base_url, credential_ref, enabled, health_status, safe_metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, project_id::text, name, kind, base_url, credential_ref, enabled, health_status, safe_metadata, created_at, updated_at
	`, input.ProjectID, input.Name, input.Kind, input.BaseURL, input.CredentialRef, input.Enabled, health, objectJSON(input.SafeMetadata)))
}

func (s *Store) CreateModelProfile(ctx context.Context, input store.ModelProfile) (store.ModelProfile, error) {
	return scanModelProfile(s.pool.QueryRow(ctx, `
		INSERT INTO model_profiles (project_id, provider_id, name, model, temperature, max_tokens, max_concurrent, generation_settings, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, project_id::text, provider_id::text, name, model, temperature, max_tokens, max_concurrent, generation_settings, enabled, created_at, updated_at
	`, input.ProjectID, input.ProviderID, input.Name, input.Model, input.Temperature, input.MaxTokens, input.MaxConcurrent, objectJSON(input.GenerationSettings), input.Enabled))
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
		INSERT INTO agents (project_id, name, role_instructions, engine, model_profile_id, engine_settings, concurrency_limit, state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, project_id::text, name, role_instructions, engine, model_profile_id::text, engine_settings, concurrency_limit, state, created_at, updated_at
	`, input.ProjectID, input.Name, input.RoleInstructions, input.Engine, input.ModelProfileID, objectJSON(input.EngineSettings), limit, state))
}

func (s *Store) GetAgent(ctx context.Context, projectID, agentID string) (store.Agent, error) {
	return scanAgent(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, name, role_instructions, engine, model_profile_id::text, engine_settings, concurrency_limit, state, created_at, updated_at
		FROM agents
		WHERE id = $2 AND (project_id IS NULL OR project_id = $1)
	`, projectID, agentID))
}

func scanProvider(row pgx.Row) (store.Provider, error) {
	var value store.Provider
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Name, &value.Kind, &value.BaseURL, &value.CredentialRef, &value.Enabled, &value.HealthStatus, &value.SafeMetadata, &value.CreatedAt, &value.UpdatedAt); err != nil {
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

func scanAgent(row pgx.Row) (store.Agent, error) {
	var value store.Agent
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Name, &value.RoleInstructions, &value.Engine, &value.ModelProfileID, &value.EngineSettings, &value.ConcurrencyLimit, &value.State, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Agent{}, notFound(err)
	}
	return value, nil
}
