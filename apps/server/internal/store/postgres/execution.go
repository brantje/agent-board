package postgres

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateWorkspace(ctx context.Context, input store.Workspace) (store.Workspace, error) {
	status := input.BootstrapStatus
	if status == "" {
		status = "PENDING"
	}
	return scanWorkspace(s.pool.QueryRow(ctx, `
		INSERT INTO workspaces (project_id, issue_id, path, repository_path, base_branch, base_revision, working_branch, bootstrap_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, bootstrap_status, created_at, updated_at
	`, input.ProjectID, input.IssueID, input.Path, input.RepositoryPath, input.BaseBranch, input.BaseRevision, input.WorkingBranch, status))
}

func (s *Store) GetWorkspaceByIssue(ctx context.Context, projectID, issueID string) (store.Workspace, error) {
	return scanWorkspace(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, bootstrap_status, created_at, updated_at
		FROM workspaces WHERE project_id = $1 AND issue_id = $2
	`, projectID, issueID))
}

func (s *Store) CreateRun(ctx context.Context, input store.Run) (store.Run, error) {
	status := input.Status
	if status == "" {
		status = "QUEUED"
	}
	return scanRun(s.pool.QueryRow(ctx, `
		INSERT INTO runs (project_id, issue_id, workspace_id, agent_id, attempt, status, queue_reason, failure_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, input.ProjectID, input.IssueID, input.WorkspaceID, input.AgentID, input.Attempt, status, input.QueueReason, input.FailureReason))
}

func (s *Store) GetRun(ctx context.Context, projectID, runID string) (store.Run, error) {
	return scanRun(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs WHERE project_id = $1 AND id = $2
	`, projectID, runID))
}

func (s *Store) CreateRuntimeInstance(ctx context.Context, input store.RuntimeInstance) (store.RuntimeInstance, error) {
	status := input.Status
	if status == "" {
		status = "PROVISIONING"
	}
	runnerStatus := input.RunnerStatus
	if runnerStatus == "" {
		runnerStatus = "CONNECTING"
	}
	return scanRuntimeInstance(s.pool.QueryRow(ctx, `
		INSERT INTO runtime_instances (project_id, workspace_id, runtime_id, status, external_id, runner_status, safe_handle_metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text, project_id::text, workspace_id::text, runtime_id::text, status, external_id, runner_status, safe_handle_metadata, created_at, started_at, stopped_at, updated_at
	`, input.ProjectID, input.WorkspaceID, input.RuntimeID, status, input.ExternalID, runnerStatus, objectJSON(input.SafeHandleMetadata)))
}

func (s *Store) UpdateRuntimeInstanceState(ctx context.Context, projectID, instanceID, status string, externalID *string, runnerStatus string, safeMetadata json.RawMessage) (store.RuntimeInstance, error) {
	return scanRuntimeInstance(s.pool.QueryRow(ctx, `
		UPDATE runtime_instances
		SET status = $3,
		    external_id = $4,
		    runner_status = $5,
		    safe_handle_metadata = $6,
		    started_at = CASE WHEN $3 = 'RUNNING' AND started_at IS NULL THEN now() ELSE started_at END,
		    stopped_at = CASE WHEN $3 IN ('STOPPED', 'DESTROYED', 'FAILED') THEN now() ELSE stopped_at END,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING id::text, project_id::text, workspace_id::text, runtime_id::text, status, external_id, runner_status, safe_handle_metadata, created_at, started_at, stopped_at, updated_at
	`, projectID, instanceID, status, externalID, runnerStatus, objectJSON(safeMetadata)))
}

func (s *Store) CreateExecutionSession(ctx context.Context, input store.ExecutionSession) (store.ExecutionSession, error) {
	status := input.Status
	if status == "" {
		status = "PENDING"
	}
	cwd := input.CWD
	if cwd == "" {
		cwd = "/workspace"
	}
	return scanExecutionSession(s.pool.QueryRow(ctx, `
		INSERT INTO execution_sessions (project_id, run_id, runtime_instance_id, status, cwd, command_argv, exit_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text, project_id::text, run_id::text, runtime_instance_id::text, status, cwd, command_argv, exit_code, created_at, started_at, completed_at, updated_at
	`, input.ProjectID, input.RunID, input.RuntimeInstanceID, status, cwd, arrayJSON(input.CommandArgv), input.ExitCode))
}

func (s *Store) CreateQuestion(ctx context.Context, input store.Question) (store.Question, error) {
	kind := input.Kind
	if kind == "" {
		kind = "TEXT"
	}
	status := input.Status
	if status == "" {
		status = "OPEN"
	}
	return scanQuestion(s.pool.QueryRow(ctx, `
		INSERT INTO questions (project_id, issue_id, run_id, prompt, kind, options, recommendation, blocking, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, blocking, status, created_at, answered_at
	`, input.ProjectID, input.IssueID, input.RunID, input.Prompt, kind, arrayJSON(input.Options), input.Recommendation, input.Blocking, status))
}

func (s *Store) CreateDecision(ctx context.Context, input store.Decision) (store.Decision, error) {
	return scanDecision(s.pool.QueryRow(ctx, `
		INSERT INTO decisions (project_id, issue_id, run_id, question_id, kind, outcome, actor_type, actor_id, safe_details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, question_id::text, kind, outcome, actor_type, actor_id, safe_details, created_at
	`, input.ProjectID, input.IssueID, input.RunID, input.QuestionID, input.Kind, input.Outcome, input.ActorType, input.ActorID, objectJSON(input.SafeDetails)))
}

func (s *Store) CreateReview(ctx context.Context, input store.Review) (store.Review, error) {
	status := input.Status
	if status == "" {
		status = "PENDING"
	}
	return scanReview(s.pool.QueryRow(ctx, `
		INSERT INTO reviews (project_id, issue_id, run_id, status, decision_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text, requested_at, decided_at, created_at, updated_at
	`, input.ProjectID, input.IssueID, input.RunID, status, input.DecisionID))
}

func scanWorkspace(row pgx.Row) (store.Workspace, error) {
	var value store.Workspace
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.Path, &value.RepositoryPath, &value.BaseBranch, &value.BaseRevision, &value.WorkingBranch, &value.BootstrapStatus, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Workspace{}, notFound(err)
	}
	return value, nil
}

func scanRun(row pgx.Row) (store.Run, error) {
	var value store.Run
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.WorkspaceID, &value.AgentID, &value.Attempt, &value.Status, &value.QueueReason, &value.FailureReason, &value.CreatedAt, &value.StartedAt, &value.CompletedAt, &value.UpdatedAt); err != nil {
		return store.Run{}, notFound(err)
	}
	return value, nil
}

func scanRuntimeInstance(row pgx.Row) (store.RuntimeInstance, error) {
	var value store.RuntimeInstance
	if err := row.Scan(&value.ID, &value.ProjectID, &value.WorkspaceID, &value.RuntimeID, &value.Status, &value.ExternalID, &value.RunnerStatus, &value.SafeHandleMetadata, &value.CreatedAt, &value.StartedAt, &value.StoppedAt, &value.UpdatedAt); err != nil {
		return store.RuntimeInstance{}, notFound(err)
	}
	return value, nil
}

func scanExecutionSession(row pgx.Row) (store.ExecutionSession, error) {
	var value store.ExecutionSession
	if err := row.Scan(&value.ID, &value.ProjectID, &value.RunID, &value.RuntimeInstanceID, &value.Status, &value.CWD, &value.CommandArgv, &value.ExitCode, &value.CreatedAt, &value.StartedAt, &value.CompletedAt, &value.UpdatedAt); err != nil {
		return store.ExecutionSession{}, notFound(err)
	}
	return value, nil
}

func scanQuestion(row pgx.Row) (store.Question, error) {
	var value store.Question
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.RunID, &value.Prompt, &value.Kind, &value.Options, &value.Recommendation, &value.Blocking, &value.Status, &value.CreatedAt, &value.AnsweredAt); err != nil {
		return store.Question{}, notFound(err)
	}
	return value, nil
}

func scanDecision(row pgx.Row) (store.Decision, error) {
	var value store.Decision
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.RunID, &value.QuestionID, &value.Kind, &value.Outcome, &value.ActorType, &value.ActorID, &value.SafeDetails, &value.CreatedAt); err != nil {
		return store.Decision{}, notFound(err)
	}
	return value, nil
}

func scanReview(row pgx.Row) (store.Review, error) {
	var value store.Review
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.RunID, &value.Status, &value.DecisionID, &value.RequestedAt, &value.DecidedAt, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Review{}, notFound(err)
	}
	return value, nil
}
