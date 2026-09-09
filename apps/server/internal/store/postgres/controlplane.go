package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListProjects(ctx context.Context) ([]store.Project, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, name, issue_prefix, repository_path, default_branch, workflow_settings, allow_internal_runner, created_at, updated_at FROM projects ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Project
	for rows.Next() {
		value, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProject(ctx context.Context, input store.Project) (store.Project, error) {
	return scanProject(s.pool.QueryRow(ctx, `
		UPDATE projects SET name=$2, repository_path=$3, default_branch=$4, workflow_settings=$5, allow_internal_runner=COALESCE($6,allow_internal_runner), updated_at=now()
		WHERE id=$1
		RETURNING id::text, name, issue_prefix, repository_path, default_branch, workflow_settings, allow_internal_runner, created_at, updated_at
	`, input.ID, input.Name, input.RepositoryPath, input.DefaultBranch, objectJSON(input.WorkflowSettings), input.AllowInternalRunner))
}

func (s *Store) ListIssues(ctx context.Context, projectID string) ([]store.Issue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+issueSelectColumns+`, `+issueCurrentBranchColumn+`, `+lastEventSelectColumns+`
		FROM issues AS i
		JOIN projects AS p ON p.id = i.project_id
		`+issueWorkspaceJoin+`
		`+lastEventLateralJoin+`
		WHERE i.project_id=$1
		ORDER BY i.created_at, i.id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Issue
	for rows.Next() {
		value, err := scanIssueJoinedWithLastEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) UpdateIssue(ctx context.Context, input store.Issue) (store.Issue, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.Issue{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var issueID, previousStatus string
	if err := tx.QueryRow(ctx, `
		SELECT id::text, status
		FROM issues
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, input.ProjectID, input.ID).Scan(&issueID, &previousStatus); err != nil {
		return store.Issue{}, notFound(err)
	}

	if input.Status == "DONE" {
		var active bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM runs
				WHERE project_id=$1 AND issue_id=$2
				  AND status = ANY($3::text[])
			) OR EXISTS (
				SELECT 1
				FROM scheduler_jobs AS job
				JOIN runs AS run ON run.id=job.run_id AND run.project_id=job.project_id
				WHERE run.project_id=$1 AND run.issue_id=$2
				  AND job.state IN ('QUEUED','CLAIMED')
			)
		`, input.ProjectID, input.ID, activeRunStatuses).Scan(&active); err != nil {
			return store.Issue{}, err
		}
		if active {
			return store.Issue{}, store.ErrConflict
		}
	}

	updated, err := scanIssueJoined(tx.QueryRow(ctx, `
		UPDATE issues AS i SET title=$3, description=$4, status=$5, priority=$6, assigned_agent_id=$7, updated_at=now()
		FROM projects AS p
		WHERE i.project_id=$1 AND i.id=$2 AND p.id=i.project_id
		RETURNING `+issueSelectColumns+`
	`, input.ProjectID, input.ID, input.Title, input.Description, input.Status, input.Priority, input.AssignedAgentID))
	if err != nil {
		return store.Issue{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Issue{}, err
	}
	updated.PreviousStatus = previousStatus
	return updated, nil
}

func (s *Store) ListRuns(ctx context.Context, projectID string) ([]store.Run, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+runSelectColumns+`
		FROM runs AS r
		`+runWorkspaceJoin+`
		WHERE r.project_id=$1
		ORDER BY r.created_at DESC, r.id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Run
	for rows.Next() {
		value, err := scanRunWithBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) ListProviders(ctx context.Context, projectID *string) ([]store.Provider, error) {
	project, scoped := visibleScope(projectID)
	rows, err := s.pool.Query(ctx, `SELECT id::text, project_id::text, name, kind, base_url, credential_ref, enabled, health_status, safe_metadata, created_at, updated_at FROM providers WHERE ($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL) ORDER BY project_id NULLS FIRST, created_at, id`, nullableUUID(project, scoped), scoped)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Provider
	for rows.Next() {
		value, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) GetProvider(ctx context.Context, projectID *string, id string) (store.Provider, error) {
	project, scoped := visibleScope(projectID)
	return scanProvider(s.pool.QueryRow(ctx, `SELECT id::text, project_id::text, name, kind, base_url, credential_ref, enabled, health_status, safe_metadata, created_at, updated_at FROM providers WHERE id=$3 AND (($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL))`, nullableUUID(project, scoped), scoped, id))
}

func (s *Store) UpdateProvider(ctx context.Context, scope *string, input store.Provider) (store.Provider, error) {
	return scanProvider(s.pool.QueryRow(ctx, `
		UPDATE providers SET name=$3, kind=$4, base_url=$5, credential_ref=$6, enabled=$7, safe_metadata=$8, updated_at=now()
		WHERE id=$2 AND project_id IS NOT DISTINCT FROM $1::uuid
		RETURNING id::text, project_id::text, name, kind, base_url, credential_ref, enabled, health_status, safe_metadata, created_at, updated_at
	`, scope, input.ID, input.Name, input.Kind, input.BaseURL, input.CredentialRef, input.Enabled, objectJSON(input.SafeMetadata)))
}

func visibleScope(projectID *string) (string, bool) {
	if projectID == nil {
		return "", false
	}
	return *projectID, true
}

func nullableUUID(value string, present bool) any {
	if !present {
		return nil
	}
	return value
}

func (s *Store) ListModelProfiles(ctx context.Context, projectID *string) ([]store.ModelProfile, error) {
	project, scoped := visibleScope(projectID)
	rows, err := s.pool.Query(ctx, `SELECT id::text, project_id::text, provider_id::text, name, model, temperature, max_tokens, max_concurrent, generation_settings, enabled, created_at, updated_at FROM model_profiles WHERE ($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL) ORDER BY project_id NULLS FIRST, created_at, id`, nullableUUID(project, scoped), scoped)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.ModelProfile
	for rows.Next() {
		value, err := scanModelProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) GetModelProfile(ctx context.Context, projectID *string, id string) (store.ModelProfile, error) {
	project, scoped := visibleScope(projectID)
	return scanModelProfile(s.pool.QueryRow(ctx, `SELECT id::text, project_id::text, provider_id::text, name, model, temperature, max_tokens, max_concurrent, generation_settings, enabled, created_at, updated_at FROM model_profiles WHERE id=$3 AND (($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL))`, nullableUUID(project, scoped), scoped, id))
}

func (s *Store) UpdateModelProfile(ctx context.Context, scope *string, input store.ModelProfile) (store.ModelProfile, error) {
	return scanModelProfile(s.pool.QueryRow(ctx, `UPDATE model_profiles SET provider_id=$3, name=$4, model=$5, temperature=$6, max_tokens=$7, max_concurrent=$8, generation_settings=$9, enabled=$10, updated_at=now() WHERE id=$2 AND project_id IS NOT DISTINCT FROM $1::uuid RETURNING id::text, project_id::text, provider_id::text, name, model, temperature, max_tokens, max_concurrent, generation_settings, enabled, created_at, updated_at`, scope, input.ID, input.ProviderID, input.Name, input.Model, input.Temperature, input.MaxTokens, input.MaxConcurrent, objectJSON(input.GenerationSettings), input.Enabled))
}

func (s *Store) ListRuntimes(ctx context.Context, projectID *string) ([]store.Runtime, error) {
	project, scoped := visibleScope(projectID)
	rows, err := s.pool.Query(ctx, `SELECT id::text, project_id::text, name, kind, image, cpu_limit_millis, memory_limit_bytes, pid_limit, timeout_seconds, network_policy, workspace_policy, allowed_secret_refs, capabilities, enabled, health_status, created_at, updated_at FROM runtimes WHERE ($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL) ORDER BY project_id NULLS FIRST, created_at, id`, nullableUUID(project, scoped), scoped)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Runtime
	for rows.Next() {
		value, err := scanRuntime(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) GetRuntime(ctx context.Context, projectID *string, id string) (store.Runtime, error) {
	project, scoped := visibleScope(projectID)
	return scanRuntime(s.pool.QueryRow(ctx, `SELECT id::text, project_id::text, name, kind, image, cpu_limit_millis, memory_limit_bytes, pid_limit, timeout_seconds, network_policy, workspace_policy, allowed_secret_refs, capabilities, enabled, health_status, created_at, updated_at FROM runtimes WHERE id=$3 AND (($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL))`, nullableUUID(project, scoped), scoped, id))
}

func (s *Store) UpdateRuntime(ctx context.Context, scope *string, input store.Runtime) (store.Runtime, error) {
	allowedSecretRefs := input.AllowedSecretRefs
	if allowedSecretRefs == nil {
		allowedSecretRefs = []string{}
	}
	return scanRuntime(s.pool.QueryRow(ctx, `UPDATE runtimes SET name=$3, kind=$4, image=$5, cpu_limit_millis=$6, memory_limit_bytes=$7, pid_limit=$8, timeout_seconds=$9, network_policy=$10, workspace_policy=$11, allowed_secret_refs=$12, capabilities=$13, enabled=$14, updated_at=now() WHERE id=$2 AND project_id IS NOT DISTINCT FROM $1::uuid RETURNING id::text, project_id::text, name, kind, image, cpu_limit_millis, memory_limit_bytes, pid_limit, timeout_seconds, network_policy, workspace_policy, allowed_secret_refs, capabilities, enabled, health_status, created_at, updated_at`, scope, input.ID, input.Name, input.Kind, input.Image, input.CPULimitMillis, input.MemoryLimitBytes, input.PIDLimit, input.TimeoutSeconds, input.NetworkPolicy, input.WorkspacePolicy, allowedSecretRefs, objectJSON(input.Capabilities), input.Enabled))
}

func (s *Store) ListAgents(ctx context.Context, projectID *string) ([]store.Agent, error) {
	project, scoped := visibleScope(projectID)
	rows, err := s.pool.Query(ctx, `SELECT id::text, project_id::text, name, role_instructions, engine, model_profile_id::text, engine_settings, concurrency_limit, state, created_at, updated_at FROM agents WHERE ($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL) ORDER BY project_id NULLS FIRST, created_at, id`, nullableUUID(project, scoped), scoped)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Agent
	for rows.Next() {
		value, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) GetAgentInScope(ctx context.Context, projectID *string, id string) (store.Agent, error) {
	project, scoped := visibleScope(projectID)
	return scanAgent(s.pool.QueryRow(ctx, `SELECT id::text, project_id::text, name, role_instructions, engine, model_profile_id::text, engine_settings, concurrency_limit, state, created_at, updated_at FROM agents WHERE id=$3 AND (($2::boolean AND (project_id IS NULL OR project_id=$1::uuid)) OR (NOT $2::boolean AND project_id IS NULL))`, nullableUUID(project, scoped), scoped, id))
}

func (s *Store) UpdateAgent(ctx context.Context, scope *string, input store.Agent) (store.Agent, error) {
	return scanAgent(s.pool.QueryRow(ctx, `UPDATE agents SET name=$3, role_instructions=$4, engine=$5, model_profile_id=$6, engine_settings=$7, concurrency_limit=$8, state=$9, updated_at=now() WHERE id=$2 AND project_id IS NOT DISTINCT FROM $1::uuid RETURNING id::text, project_id::text, name, role_instructions, engine, model_profile_id::text, engine_settings, concurrency_limit, state, created_at, updated_at`, scope, input.ID, input.Name, input.RoleInstructions, input.Engine, input.ModelProfileID, objectJSON(input.EngineSettings), input.ConcurrencyLimit, input.State))
}
