package postgres

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

type assigneeQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const eligibleAgentAssigneePredicate = `a.state='ENABLED' AND (a.project_id IS NULL OR a.project_id=$1)`

// Resolve both directory entries and mutation targets through the same eligibility
// check. Execution readiness is deliberately outside ownership validation.
func resolveAssignee(ctx context.Context, q assigneeQuerier, projectID string, target *store.Assignee) (*store.Assignee, error) {
	if target == nil {
		return nil, nil
	}
	value := &store.Assignee{Type: target.Type, ID: target.ID}
	switch target.Type {
	case "AGENT":
		var state string
		var eligible bool
		err := q.QueryRow(ctx, `SELECT a.id::text,a.name,a.state,(`+eligibleAgentAssigneePredicate+`) FROM agents AS a WHERE a.id=$2`, projectID, target.ID).Scan(&value.ID, &value.Name, &state, &eligible)
		if err != nil {
			return nil, notFound(err)
		}
		if !eligible {
			if state == "ENABLED" {
				return nil, store.ErrNotFound
			}
			return nil, store.ErrInvalidArgument
		}
	case "USER":
		name, err := resolveProjectWorkflowUser(ctx, q, projectID, target.ID)
		if err != nil {
			return nil, err
		}
		value.Name = name
	case "SQUAD":
		var state string
		var inProject bool
		err := q.QueryRow(ctx, `
			SELECT sq.id::text, sq.name, a.state, sq.project_id=$1
			FROM squads AS sq
			JOIN agents AS a ON a.id=sq.leader_agent_id
			WHERE sq.id=$2
		`, projectID, target.ID).Scan(&value.ID, &value.Name, &state, &inProject)
		if err != nil {
			return nil, notFound(err)
		}
		if !inProject {
			return nil, store.ErrNotFound
		}
		if state != "ENABLED" {
			return nil, store.ErrInvalidArgument
		}
	default:
		return nil, store.ErrInvalidArgument
	}
	return value, nil
}

func (s *Store) ListIssueAssignees(ctx context.Context, projectID string) ([]store.Assignee, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT type,id,name
		FROM (
			SELECT 'USER'::text AS type,u.id::text AS id,u.display_name AS name
			FROM users AS u
			WHERE `+eligibleProjectWorkflowUserPredicate+`
			UNION ALL
			SELECT 'AGENT'::text,a.id::text,a.name
			FROM agents AS a
			WHERE `+eligibleAgentAssigneePredicate+`
			UNION ALL
			SELECT 'SQUAD'::text,sq.id::text,sq.name
			FROM squads AS sq
			JOIN agents AS a ON a.id=sq.leader_agent_id
			WHERE sq.project_id=$1 AND a.state='ENABLED' AND (a.project_id IS NULL OR a.project_id=$1)
		) assignees
		ORDER BY name,type,id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []store.Assignee{}
	for rows.Next() {
		var assignee store.Assignee
		if err := rows.Scan(&assignee.Type, &assignee.ID, &assignee.Name); err != nil {
			return nil, err
		}
		result = append(result, assignee)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) SetIssueAssignee(ctx context.Context, projectID, issueID string, target *store.Assignee, actor json.RawMessage) (store.IssueMutationResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockAssigneeEligibility(ctx, tx); err != nil {
		return store.IssueMutationResult{}, err
	}
	issue, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, projectID, issueID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	assignee, err := resolveAssignee(ctx, tx, projectID, target)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	current := issue.AssignedTo()
	if (current == nil && assignee == nil) || (current != nil && assignee != nil && current.Type == assignee.Type && current.ID == assignee.ID) {
		issue, err = getIssue(ctx, tx, projectID, issueID)
		if err != nil {
			return store.IssueMutationResult{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return store.IssueMutationResult{}, err
		}
		return store.IssueMutationResult{Issue: issue}, nil
	}
	var kind, id *string
	if assignee != nil {
		kind = &assignee.Type
		id = &assignee.ID
	}
	issue, err = scanIssueJoined(tx.QueryRow(ctx, `
        UPDATE issues AS i SET assignee_type=$3,assignee_id=$4,updated_at=now()
        FROM projects p
        WHERE i.project_id=$1 AND i.id=$2 AND p.id=i.project_id
        RETURNING `+issueSelectColumns, projectID, issueID, kind, id))
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	payload, err := json.Marshal(map[string]any{"assignedTo": assignee})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	assignmentEvent, err := appendEventTx(ctx, tx, store.Event{Type: "issue.assigned", ProjectID: projectID, IssueID: &issueID, Actor: actor, Payload: payload})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	_, runEvent, err := s.enqueueIssueMutation(ctx, tx, issue, issue.Status, true, repositoryPath, defaultBranch)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	events := []store.Event{assignmentEvent}
	issue.LastEvent = &assignmentEvent
	if runEvent.ID != "" {
		events = append(events, runEvent)
		issue.LastEvent = &runEvent
	}
	if err = tx.Commit(ctx); err != nil {
		return store.IssueMutationResult{}, err
	}
	return store.IssueMutationResult{Issue: issue, Events: events}, nil
}

func lockAssigneeEligibility(ctx context.Context, tx pgx.Tx) error {
	if err := lockProjectWorkflowUserEligibility(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `LOCK TABLE agents, squads IN SHARE MODE`)
	return err
}
