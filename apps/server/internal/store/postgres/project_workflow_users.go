package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const eligibleProjectWorkflowUserPredicate = `u.status='active' AND COALESCE((` + effectiveProjectRoleExpression + `),'') IN ('member','admin')`

func resolveProjectWorkflowUser(ctx context.Context, q assigneeQuerier, projectID, userID string) (string, error) {
	var (
		name     string
		status   string
		eligible bool
	)
	if err := q.QueryRow(ctx, `
		SELECT u.display_name,u.status,(`+eligibleProjectWorkflowUserPredicate+`)
		FROM users AS u
		WHERE u.id=$2
	`, projectID, userID).Scan(&name, &status, &eligible); err != nil {
		return "", notFound(err)
	}
	if status != store.UserStatusActive || !eligible {
		return "", store.ErrInvalidArgument
	}
	return name, nil
}

func (s *Store) ValidateProjectWorkflowUser(ctx context.Context, projectID, userID string) error {
	_, err := resolveProjectWorkflowUser(ctx, s.pool, projectID, userID)
	return err
}

func lockProjectWorkflowUserEligibility(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `LOCK TABLE users, project_user_access, project_group_access, group_members IN SHARE MODE`)
	return err
}

var _ store.ProjectWorkflowUserEligibilityStore = (*Store)(nil)
