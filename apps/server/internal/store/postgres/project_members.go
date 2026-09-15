package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *Store) ListProjectMembers(ctx context.Context, projectID string) ([]store.ProjectMember, error) {
	rows, err := s.pool.Query(ctx, `
		WITH effective_members AS (
			SELECT
				u.id::text AS user_id,
				u.username,
				u.display_name,
				`+effectiveProjectRoleExpression+` AS role
			FROM users AS u
			JOIN projects AS p ON p.id=$1
			WHERE u.status=$2
		)
		SELECT user_id, username, display_name, role
		FROM effective_members
		WHERE role IS NOT NULL
		ORDER BY display_name, username, user_id
	`, projectID, store.UserStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]store.ProjectMember, 0)
	for rows.Next() {
		var member store.ProjectMember
		if err := rows.Scan(&member.UserID, &member.Username, &member.DisplayName, &member.Role); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return members, nil
}
