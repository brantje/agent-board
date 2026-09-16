package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const squadColumns = `id::text,project_id::text,name,leader_agent_id::text,created_at,updated_at`

type squadQueryFunc func(context.Context, string, ...any) (pgx.Rows, error)

func scanSquad(row pgx.Row) (store.Squad, error) {
	var squad store.Squad
	if err := row.Scan(&squad.ID, &squad.ProjectID, &squad.Name, &squad.LeaderAgentID, &squad.CreatedAt, &squad.UpdatedAt); err != nil {
		return store.Squad{}, notFound(err)
	}
	return squad, nil
}

func loadSquadMembers(ctx context.Context, query squadQueryFunc, squadID string) ([]store.SquadMember, error) {
	rows, err := query(ctx, `
		SELECT agent_id::text, role
		FROM squad_members
		WHERE squad_id=$1
		ORDER BY agent_id
	`, squadID)
	if err != nil {
		return nil, notFound(err)
	}
	defer rows.Close()

	members := make([]store.SquadMember, 0)
	for rows.Next() {
		var member store.SquadMember
		if err := rows.Scan(&member.AgentID, &member.Role); err != nil {
			return nil, notFound(err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, notFound(err)
	}
	return members, nil
}

func (s *Store) CreateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.Squad{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	squad, err := scanSquad(tx.QueryRow(ctx, `
		INSERT INTO squads (project_id,name,leader_agent_id)
		VALUES ($1,$2,$3)
		RETURNING `+squadColumns,
		input.ProjectID, input.Name, input.LeaderAgentID,
	))
	if err != nil {
		return store.Squad{}, err
	}
	for _, member := range input.Members {
		if _, err := tx.Exec(ctx, `
			INSERT INTO squad_members (squad_id,agent_id,role)
			VALUES ($1,$2,$3)
		`, squad.ID, member.AgentID, member.Role); err != nil {
			return store.Squad{}, notFound(err)
		}
	}
	squad.Members, err = loadSquadMembers(ctx, tx.Query, squad.ID)
	if err != nil {
		return store.Squad{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Squad{}, notFound(err)
	}
	return squad, nil
}

func (s *Store) GetSquad(ctx context.Context, projectID, squadID string) (store.Squad, error) {
	squad, err := scanSquad(s.pool.QueryRow(ctx, `
		SELECT `+squadColumns+`
		FROM squads
		WHERE project_id=$1 AND id=$2
	`, projectID, squadID))
	if err != nil {
		return store.Squad{}, err
	}
	squad.Members, err = loadSquadMembers(ctx, s.pool.Query, squad.ID)
	if err != nil {
		return store.Squad{}, err
	}
	return squad, nil
}

func (s *Store) ListSquads(ctx context.Context, projectID string) ([]store.Squad, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+squadColumns+`
		FROM squads
		WHERE project_id=$1
		ORDER BY created_at,id
	`, projectID)
	if err != nil {
		return nil, notFound(err)
	}

	squads := make([]store.Squad, 0)
	for rows.Next() {
		squad, err := scanSquad(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		squads = append(squads, squad)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, notFound(err)
	}
	rows.Close()

	for i := range squads {
		squads[i].Members, err = loadSquadMembers(ctx, s.pool.Query, squads[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return squads, nil
}

func (s *Store) UpdateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.Squad{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := scanSquad(tx.QueryRow(ctx, `
		SELECT `+squadColumns+`
		FROM squads
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, input.ProjectID, input.ID)); err != nil {
		return store.Squad{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM squad_members WHERE squad_id=$1`, input.ID); err != nil {
		return store.Squad{}, notFound(err)
	}

	squad, err := scanSquad(tx.QueryRow(ctx, `
		UPDATE squads
		SET name=$3,leader_agent_id=$4,updated_at=now()
		WHERE project_id=$1 AND id=$2
		RETURNING `+squadColumns,
		input.ProjectID, input.ID, input.Name, input.LeaderAgentID,
	))
	if err != nil {
		return store.Squad{}, err
	}
	for _, member := range input.Members {
		if _, err := tx.Exec(ctx, `
			INSERT INTO squad_members (squad_id,agent_id,role)
			VALUES ($1,$2,$3)
		`, squad.ID, member.AgentID, member.Role); err != nil {
			return store.Squad{}, notFound(err)
		}
	}
	squad.Members, err = loadSquadMembers(ctx, tx.Query, squad.ID)
	if err != nil {
		return store.Squad{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Squad{}, notFound(err)
	}
	return squad, nil
}

func (s *Store) DeleteSquad(ctx context.Context, projectID, squadID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM squads WHERE project_id=$1 AND id=$2`, projectID, squadID)
	if err != nil {
		return notFound(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

var _ store.SquadStore = (*Store)(nil)
