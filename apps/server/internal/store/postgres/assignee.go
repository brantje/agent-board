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

const eligibleUserAssigneePredicate = `u.status='active' AND (
	u.deployment_role='admin'
	OR EXISTS (
		SELECT 1 FROM project_user_access AS pua
		WHERE pua.project_id=$1 AND pua.user_id=u.id AND pua.role IN ('member','admin')
	)
	OR EXISTS (
		SELECT 1
		FROM group_members AS gm
		JOIN project_group_access AS pga ON pga.group_id=gm.group_id
		WHERE gm.user_id=u.id AND pga.project_id=$1 AND pga.role IN ('member','admin')
	)
)`

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
		var status string
		var eligible bool
		err := q.QueryRow(ctx, `SELECT u.id::text,u.display_name,u.status,(`+eligibleUserAssigneePredicate+`) FROM users AS u WHERE u.id=$2`, projectID, target.ID).Scan(&value.ID, &value.Name, &status, &eligible)
		if err != nil {
			return nil, notFound(err)
		}
		if status != store.UserStatusActive || !eligible {
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
			WHERE `+eligibleUserAssigneePredicate+`
			UNION ALL
			SELECT 'AGENT'::text,a.id::text,a.name
			FROM agents AS a
			WHERE `+eligibleAgentAssigneePredicate+`
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
	// Serialize eligibility checks with changes to Users, grants, memberships and
	// Agent scope/state, using the existing administration lock order.
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
	_, runEvent, err := enqueueIssueMutation(ctx, tx, issue, issue.Status, true, repositoryPath, defaultBranch)
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
	_, err := tx.Exec(ctx, `LOCK TABLE users, project_user_access, project_group_access, group_members, agents IN SHARE MODE`)
	return err
}
