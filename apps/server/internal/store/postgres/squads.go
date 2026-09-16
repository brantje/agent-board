package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const squadSelectColumns = `id::text, project_id::text, name, leader_agent_id::text, created_at, updated_at`

func (s *Store) CreateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Squad{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	value, err := scanSquad(tx.QueryRow(ctx, `
		INSERT INTO squads (project_id, name, leader_agent_id)
		VALUES ($1, $2, $3)
		RETURNING `+squadSelectColumns+`
	`, input.ProjectID, input.Name, input.LeaderAgentID))
	if err != nil {
		return store.Squad{}, notFound(err)
	}
	if err := insertSquadMembers(ctx, tx, value.ID, input.Members); err != nil {
		return store.Squad{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Squad{}, err
	}
	value.Members = cloneSquadMembers(input.Members)
	return value, nil
}

func (s *Store) GetSquad(ctx context.Context, projectID, squadID string) (store.Squad, error) {
	value, err := scanSquad(s.pool.QueryRow(ctx, `
		SELECT `+squadSelectColumns+`
		FROM squads
		WHERE project_id = $1 AND id = $2
	`, projectID, squadID))
	if err != nil {
		return store.Squad{}, err
	}
	members, err := listSquadMembers(ctx, s.pool, squadID)
	if err != nil {
		return store.Squad{}, err
	}
	value.Members = members
	return value, nil
}

func (s *Store) ListSquads(ctx context.Context, projectID string) ([]store.Squad, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+squadSelectColumns+`
		FROM squads
		WHERE project_id = $1
		ORDER BY lower(name), id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]store.Squad, 0)
	for rows.Next() {
		value, err := scanSquad(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	for i := range values {
		members, err := listSquadMembers(ctx, s.pool, values[i].ID)
		if err != nil {
			return nil, err
		}
		values[i].Members = members
	}
	return values, nil
}

func (s *Store) UpdateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Squad{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var squadID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM squads
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, input.ProjectID, input.ID).Scan(&squadID); err != nil {
		return store.Squad{}, notFound(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM squad_members WHERE squad_id = $1`, squadID); err != nil {
		return store.Squad{}, err
	}

	value, err := scanSquad(tx.QueryRow(ctx, `
		UPDATE squads
		SET name = $3, leader_agent_id = $4, updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING `+squadSelectColumns+`
	`, input.ProjectID, input.ID, input.Name, input.LeaderAgentID))
	if err != nil {
		return store.Squad{}, err
	}
	if err := insertSquadMembers(ctx, tx, value.ID, input.Members); err != nil {
		return store.Squad{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Squad{}, err
	}
	value.Members = cloneSquadMembers(input.Members)
	return value, nil
}

func (s *Store) DeleteSquad(ctx context.Context, projectID, squadID string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM squads WHERE project_id = $1 AND id = $2`, projectID, squadID)
	if err != nil {
		return notFound(err)
	}
	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

type squadQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func listSquadMembers(ctx context.Context, q squadQueryer, squadID string) ([]store.SquadMember, error) {
	rows, err := q.Query(ctx, `
		SELECT agent_id::text, role
		FROM squad_members
		WHERE squad_id = $1
		ORDER BY agent_id
	`, squadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]store.SquadMember, 0)
	for rows.Next() {
		var member store.SquadMember
		if err := rows.Scan(&member.AgentID, &member.Role); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return members, nil
}

func insertSquadMembers(ctx context.Context, tx pgx.Tx, squadID string, members []store.SquadMember) error {
	for _, member := range members {
		if _, err := tx.Exec(ctx, `
			INSERT INTO squad_members (squad_id, agent_id, role)
			VALUES ($1, $2, $3)
		`, squadID, member.AgentID, member.Role); err != nil {
			return notFound(err)
		}
	}
	return nil
}

func scanSquad(row pgx.Row) (store.Squad, error) {
	var value store.Squad
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Name, &value.LeaderAgentID, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Squad{}, notFound(err)
	}
	return value, nil
}

func cloneSquadMembers(members []store.SquadMember) []store.SquadMember {
	if members == nil {
		return []store.SquadMember{}
	}
	result := make([]store.SquadMember, len(members))
	copy(result, members)
	return result
}
