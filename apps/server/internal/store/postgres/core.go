package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const issueSelectColumns = `
	i.id::text,
	i.project_id::text,
	i.title,
	i.description,
	i.status,
	i.priority,
	i.assigned_agent_id::text,
	i.number,
	p.issue_prefix,
	i.created_at,
	i.updated_at
`

func (s *Store) CreateProject(ctx context.Context, input store.Project) (store.Project, error) {
	prefix := store.NormalizeIssuePrefix(input.IssuePrefix)
	if !store.ValidIssuePrefix(prefix) {
		return store.Project{}, store.ErrInvalidArgument
	}
	branch := input.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO projects (name, issue_prefix, repository_path, default_branch, workflow_settings)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, name, issue_prefix, repository_path, default_branch, workflow_settings, created_at, updated_at
	`, input.Name, prefix, input.RepositoryPath, branch, objectJSON(input.WorkflowSettings))
	return scanProject(row)
}

func (s *Store) GetProject(ctx context.Context, projectID string) (store.Project, error) {
	return scanProject(s.pool.QueryRow(ctx, `
		SELECT id::text, name, issue_prefix, repository_path, default_branch, workflow_settings, created_at, updated_at
		FROM projects WHERE id = $1
	`, projectID))
}

func (s *Store) CreateIssue(ctx context.Context, input store.Issue) (store.Issue, error) {
	status := input.Status
	if status == "" {
		status = "BACKLOG"
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.Issue{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var prefix string
	var number int
	if err := tx.QueryRow(ctx, `
		UPDATE projects
		SET next_issue_number = next_issue_number + 1, updated_at = now()
		WHERE id = $1
		RETURNING issue_prefix, next_issue_number - 1
	`, input.ProjectID).Scan(&prefix, &number); err != nil {
		return store.Issue{}, notFound(err)
	}

	issue, err := scanIssueWithPriority(tx.QueryRow(ctx, `
		INSERT INTO issues (project_id, number, title, description, status, priority, assigned_agent_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text, project_id::text, title, description, status, priority, assigned_agent_id::text, number, created_at, updated_at
	`, input.ProjectID, number, input.Title, input.Description, status, input.Priority, input.AssignedAgentID))
	if err != nil {
		return store.Issue{}, err
	}
	issue.Key = store.FormatIssueKey(prefix, issue.Number)

	if err := tx.Commit(ctx); err != nil {
		return store.Issue{}, err
	}
	return issue, nil
}

func (s *Store) GetIssue(ctx context.Context, projectID, issueID string) (store.Issue, error) {
	return scanIssueJoined(s.pool.QueryRow(ctx, `
		SELECT `+issueSelectColumns+`
		FROM issues AS i
		JOIN projects AS p ON p.id = i.project_id
		WHERE i.project_id = $1 AND i.id = $2
	`, projectID, issueID))
}

func (s *Store) GetIssueUUIDByKey(ctx context.Context, projectID, key string) (string, error) {
	prefix, number, err := store.ParseIssueKey(key)
	if err != nil {
		return "", store.ErrInvalidArgument
	}
	var projectPrefix string
	if err := s.pool.QueryRow(ctx, `SELECT issue_prefix FROM projects WHERE id = $1`, projectID).Scan(&projectPrefix); err != nil {
		return "", notFound(err)
	}
	if projectPrefix != prefix {
		return "", store.ErrNotFound
	}
	var issueID string
	if err := s.pool.QueryRow(ctx, `
		SELECT id::text
		FROM issues
		WHERE project_id = $1 AND number = $2
	`, projectID, number).Scan(&issueID); err != nil {
		return "", notFound(err)
	}
	return issueID, nil
}

func scanProject(row pgx.Row) (store.Project, error) {
	var value store.Project
	if err := row.Scan(&value.ID, &value.Name, &value.IssuePrefix, &value.RepositoryPath, &value.DefaultBranch, &value.WorkflowSettings, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Project{}, notFound(err)
	}
	return value, nil
}

func scanIssue(row pgx.Row) (store.Issue, error) {
	return scanIssueJoined(row)
}

func scanIssueWithPriority(row pgx.Row) (store.Issue, error) {
	var value store.Issue
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Title, &value.Description, &value.Status, &value.Priority, &value.AssignedAgentID, &value.Number, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Issue{}, notFound(err)
	}
	return value, nil
}

func scanIssueJoined(row pgx.Row) (store.Issue, error) {
	var value store.Issue
	var prefix string
	if err := row.Scan(
		&value.ID, &value.ProjectID, &value.Title, &value.Description, &value.Status, &value.Priority,
		&value.AssignedAgentID, &value.Number, &prefix, &value.CreatedAt, &value.UpdatedAt,
	); err != nil {
		return store.Issue{}, notFound(err)
	}
	value.Key = store.FormatIssueKey(prefix, value.Number)
	return value, nil
}
