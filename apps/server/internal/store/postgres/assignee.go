package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

type assigneeQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

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
		err := q.QueryRow(ctx, `SELECT id::text,name,state FROM agents WHERE id=$2 AND (project_id IS NULL OR project_id=$1)`, projectID, target.ID).Scan(&value.ID, &value.Name, &state)
		if err != nil {
			return nil, notFound(err)
		}
		if state != "ENABLED" {
			return nil, store.ErrInvalidArgument
		}
	case "USER":
		var status, deploymentRole string
		err := q.QueryRow(ctx, `SELECT id::text,display_name,status,deployment_role FROM users WHERE id=$1`, target.ID).Scan(&value.ID, &value.Name, &status, &deploymentRole)
		if err != nil {
			return nil, notFound(err)
		}
		if status != store.UserStatusActive {
			return nil, store.ErrInvalidArgument
		}
		if deploymentRole != store.DeploymentRoleAdmin {
			role, err := effectiveProjectRole(ctx, q, projectID, target.ID)
			if errors.Is(err, store.ErrNotFound) {
				return nil, store.ErrInvalidArgument
			}
			if err != nil {
				return nil, err
			}
			if !store.ProjectRoleAtLeast(role, store.ProjectRoleMember) {
				return nil, store.ErrInvalidArgument
			}
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
        SELECT 'USER',id::text FROM users WHERE status='active'
        UNION ALL
        SELECT 'AGENT',id::text FROM agents
        WHERE state='ENABLED' AND (project_id IS NULL OR project_id=$1)
    `, projectID)
	if err != nil {
		return nil, err
	}
	candidates := []store.Assignee{}
	for rows.Next() {
		var a store.Assignee
		if err := rows.Scan(&a.Type, &a.ID); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := []store.Assignee{}
	for _, candidate := range candidates {
		a, err := resolveAssignee(ctx, s.pool, projectID, &candidate)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, *a)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (s *Store) SetIssueAssignee(ctx context.Context, projectID, issueID string, target *store.Assignee, actor json.RawMessage) (store.Issue, store.Event, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Issue{}, store.Event{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize eligibility checks with changes to Users, grants, memberships and
	// Agent scope/state, using the existing administration lock order.
	if err = lockAssigneeEligibility(ctx, tx); err != nil {
		return store.Issue{}, store.Event{}, err
	}
	issue, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, projectID, issueID)
	if err != nil {
		return store.Issue{}, store.Event{}, err
	}
	assignee, err := resolveAssignee(ctx, tx, projectID, target)
	if err != nil {
		return store.Issue{}, store.Event{}, err
	}
	current := issue.AssignedTo()
	if (current == nil && assignee == nil) || (current != nil && assignee != nil && current.Type == assignee.Type && current.ID == assignee.ID) {
		issue, err = getIssue(ctx, tx, projectID, issueID)
		if err != nil {
			return store.Issue{}, store.Event{}, err
		}
		return issue, store.Event{}, tx.Commit(ctx)
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
		return store.Issue{}, store.Event{}, err
	}
	if _, err := enqueueIssueMutation(ctx, tx, issue, issue.Status, true, repositoryPath, defaultBranch); err != nil {
		return store.Issue{}, store.Event{}, err
	}
	payload, err := json.Marshal(map[string]any{"assignedTo": assignee})
	if err != nil {
		return store.Issue{}, store.Event{}, err
	}
	event, err := appendEventTx(ctx, tx, store.Event{Type: "issue.assigned", ProjectID: projectID, IssueID: &issueID, Actor: actor, Payload: payload})
	if err != nil {
		return store.Issue{}, store.Event{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return store.Issue{}, store.Event{}, err
	}
	issue.LastEvent = &event
	return issue, event, nil
}

func lockAssigneeEligibility(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `LOCK TABLE users, project_user_access, project_group_access, group_members, agents IN SHARE MODE`)
	return err
}
