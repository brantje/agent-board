package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const groupColumns = `id::text,name,created_at,updated_at`

func scanGroup(row pgx.Row) (store.Group, error) {
	var group store.Group
	if err := row.Scan(&group.ID, &group.Name, &group.CreatedAt, &group.UpdatedAt); err != nil {
		return store.Group{}, notFound(err)
	}
	return group, nil
}

func (s *Store) ListGroups(ctx context.Context) ([]store.Group, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+groupColumns+` FROM groups ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]store.Group, 0)
	for rows.Next() {
		group, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func (s *Store) CreateGroup(ctx context.Context, input store.Group) (store.Group, error) {
	return scanGroup(s.pool.QueryRow(ctx, `
        INSERT INTO groups (name)
        VALUES ($1)
        RETURNING `+groupColumns, input.Name))
}

func (s *Store) UpdateGroup(ctx context.Context, id, name string) (store.Group, error) {
	return scanGroup(s.pool.QueryRow(ctx, `
        UPDATE groups
        SET name=$2,updated_at=now()
        WHERE id=$1
        RETURNING `+groupColumns, id, name))
}

func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM groups WHERE id=$1`, id)
	if err != nil {
		return notFound(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListGroupMembers(ctx context.Context, groupID string) ([]store.User, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM groups WHERE id=$1)`, groupID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, store.ErrNotFound
	}

	rows, err := s.pool.Query(ctx, `
        SELECT `+userColumns+`
        FROM users u
        JOIN group_members gm ON gm.user_id=u.id
        WHERE gm.group_id=$1
        ORDER BY u.created_at,u.id
    `, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]store.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func (s *Store) AddGroupMember(ctx context.Context, groupID, userID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO group_members (group_id,user_id) VALUES ($1,$2)`, groupID, userID)
	return notFound(err)
}

func (s *Store) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`, groupID, userID)
	if err != nil {
		return notFound(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

var _ store.GroupStore = (*Store)(nil)
