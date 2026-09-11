package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const projectSelectColumns = `
	id::text,
	name,
	issue_prefix,
	source_type,
	clone_url,
	source_ref,
	repository_path,
	default_branch,
	workflow_settings,
	allow_internal_runner,
	created_at,
	updated_at
`

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

const lastEventSelectColumns = `
	last_event.id::text,
	last_event.schema_version,
	last_event.type,
	last_event.occurred_at,
	last_event.project_id::text,
	last_event.issue_id::text,
	last_event.run_id::text,
	last_event.agent_id::text,
	last_event.workspace_id::text,
	last_event.runtime_instance_id::text,
	last_event.correlation_id::text,
	last_event.parent_event_id::text,
	last_event.sequence,
	last_event.actor,
	last_event.payload,
	last_event.created_at
`

const lastEventLateralJoin = `
LEFT JOIN LATERAL (
	SELECT id, schema_version, type, occurred_at, project_id, issue_id, run_id, agent_id, workspace_id,
	       runtime_instance_id, correlation_id, parent_event_id, sequence, actor, payload, created_at
	FROM events
	WHERE project_id = i.project_id
	  AND issue_id = i.id
	  AND split_part(type, '.', 1) = ANY (ARRAY['issue','run','question','review','decision'])
	ORDER BY created_at DESC, (cmin::text)::integer DESC, id DESC
	LIMIT 1
) AS last_event ON true
`

const issueWorkspaceJoin = `
LEFT JOIN workspaces AS w ON w.project_id = i.project_id AND w.issue_id = i.id
`

const issueCurrentBranchColumn = `COALESCE(w.current_branch, w.working_branch) AS current_branch`

func (s *Store) CreateProject(ctx context.Context, input store.Project) (store.Project, error) {
	prefix := store.NormalizeIssuePrefix(input.IssuePrefix)
	if !store.ValidIssuePrefix(prefix) {
		return store.Project{}, store.ErrInvalidArgument
	}
	sourceType := input.SourceType
	if sourceType == "" {
		sourceType = store.ProjectSourceLocal
	}
	branch := input.DefaultBranch
	if sourceType == store.ProjectSourceLocal && branch == "" {
		branch = "main"
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO projects (name, issue_prefix, source_type, clone_url, source_ref, repository_path, default_branch, workflow_settings, allow_internal_runner)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9,true))
		RETURNING `+projectSelectColumns+`
	`, input.Name, prefix, sourceType, input.CloneURL, input.SourceRef, input.RepositoryPath, branch, objectJSON(input.WorkflowSettings), input.AllowInternalRunner)
	return scanProject(row)
}

func (s *Store) GetProject(ctx context.Context, projectID string) (store.Project, error) {
	return scanProject(s.pool.QueryRow(ctx, `
		SELECT `+projectSelectColumns+`
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
	return scanIssueJoinedWithLastEvent(s.pool.QueryRow(ctx, `
		SELECT `+issueSelectColumns+`, `+issueCurrentBranchColumn+`, `+lastEventSelectColumns+`
		FROM issues AS i
		JOIN projects AS p ON p.id = i.project_id
		`+issueWorkspaceJoin+`
		`+lastEventLateralJoin+`
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
	if err := row.Scan(
		&value.ID,
		&value.Name,
		&value.IssuePrefix,
		&value.SourceType,
		&value.CloneURL,
		&value.SourceRef,
		&value.RepositoryPath,
		&value.DefaultBranch,
		&value.WorkflowSettings,
		&value.AllowInternalRunner,
		&value.CreatedAt,
		&value.UpdatedAt,
	); err != nil {
		return store.Project{}, notFound(err)
	}
	return value, nil
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

func scanIssueJoinedWithLastEvent(row pgx.Row) (store.Issue, error) {
	var value store.Issue
	var prefix string
	var (
		eventID, eventType, eventProjectID                        *string
		eventIssueID, eventRunID, eventAgentID, eventWorkspaceID  *string
		eventRuntimeInstanceID, eventCorrelationID, eventParentID *string
		schemaVersion                                             *int
		sequence                                                  *int64
		occurredAt, createdAt                                     *time.Time
		actor, payload                                            json.RawMessage
	)
	if err := row.Scan(
		&value.ID, &value.ProjectID, &value.Title, &value.Description, &value.Status, &value.Priority,
		&value.AssignedAgentID, &value.Number, &prefix, &value.CreatedAt, &value.UpdatedAt,
		&value.CurrentBranch,
		&eventID, &schemaVersion, &eventType, &occurredAt, &eventProjectID, &eventIssueID, &eventRunID,
		&eventAgentID, &eventWorkspaceID, &eventRuntimeInstanceID, &eventCorrelationID, &eventParentID,
		&sequence, &actor, &payload, &createdAt,
	); err != nil {
		return store.Issue{}, notFound(err)
	}
	value.Key = store.FormatIssueKey(prefix, value.Number)
	if eventID == nil {
		return value, nil
	}
	event := store.Event{
		ID:                *eventID,
		IssueID:           eventIssueID,
		RunID:             eventRunID,
		AgentID:           eventAgentID,
		WorkspaceID:       eventWorkspaceID,
		RuntimeInstanceID: eventRuntimeInstanceID,
		CorrelationID:     eventCorrelationID,
		ParentEventID:     eventParentID,
		Sequence:          sequence,
		Actor:             actor,
		Payload:           payload,
	}
	if schemaVersion != nil {
		event.SchemaVersion = *schemaVersion
	}
	if eventType != nil {
		event.Type = *eventType
	}
	if occurredAt != nil {
		event.OccurredAt = *occurredAt
	}
	if eventProjectID != nil {
		event.ProjectID = *eventProjectID
	}
	if createdAt != nil {
		event.CreatedAt = *createdAt
	}
	if len(event.Actor) == 0 {
		event.Actor = store.EmptyObject
	}
	if len(event.Payload) == 0 {
		event.Payload = store.EmptyObject
	}
	value.LastEvent = &event
	return value, nil
}
