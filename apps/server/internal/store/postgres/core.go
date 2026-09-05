package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateProject(ctx context.Context, input store.Project) (store.Project, error) {
	branch := input.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO projects (name, repository_path, default_branch, workflow_settings)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, name, repository_path, default_branch, workflow_settings, created_at, updated_at
	`, input.Name, input.RepositoryPath, branch, objectJSON(input.WorkflowSettings))
	return scanProject(row)
}

func (s *Store) GetProject(ctx context.Context, projectID string) (store.Project, error) {
	return scanProject(s.pool.QueryRow(ctx, `
		SELECT id::text, name, repository_path, default_branch, workflow_settings, created_at, updated_at
		FROM projects WHERE id = $1
	`, projectID))
}

func (s *Store) CreateIssue(ctx context.Context, input store.Issue) (store.Issue, error) {
	status := input.Status
	if status == "" {
		status = "BACKLOG"
	}
	return scanIssue(s.pool.QueryRow(ctx, `
		INSERT INTO issues (project_id, title, description, status, assigned_agent_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, project_id::text, title, description, status, assigned_agent_id::text, created_at, updated_at
	`, input.ProjectID, input.Title, input.Description, status, input.AssignedAgentID))
}

func (s *Store) GetIssue(ctx context.Context, projectID, issueID string) (store.Issue, error) {
	return scanIssue(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, title, description, status, assigned_agent_id::text, created_at, updated_at
		FROM issues WHERE project_id = $1 AND id = $2
	`, projectID, issueID))
}

func scanProject(row pgx.Row) (store.Project, error) {
	var value store.Project
	if err := row.Scan(&value.ID, &value.Name, &value.RepositoryPath, &value.DefaultBranch, &value.WorkflowSettings, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Project{}, notFound(err)
	}
	return value, nil
}

func scanIssue(row pgx.Row) (store.Issue, error) {
	var value store.Issue
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Title, &value.Description, &value.Status, &value.AssignedAgentID, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Issue{}, notFound(err)
	}
	return value, nil
}
