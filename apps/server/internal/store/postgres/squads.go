package postgres

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const squadSelectColumns = `id::text, project_id::text, name, leader_agent_id::text, created_at, updated_at`
const squadMembersAggregate = `COALESCE((
	SELECT jsonb_agg(
		jsonb_build_object(
			'Type', CASE WHEN sm.agent_id IS NOT NULL THEN 'AGENT' ELSE 'USER' END,
			'ID', COALESCE(sm.agent_id, sm.user_id)::text,
			'Role', sm.role
		)
		ORDER BY CASE WHEN sm.agent_id IS NOT NULL THEN 'AGENT' ELSE 'USER' END, COALESCE(sm.agent_id, sm.user_id)
	)
	FROM squad_members sm
	WHERE sm.squad_id = squads.id
), '[]'::jsonb)`

func (s *Store) CreateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Squad{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := lockSquadIdentities(ctx, tx, input); err != nil {
		return store.Squad{}, err
	}
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
	return scanSquadAggregate(s.pool.QueryRow(ctx, `
		SELECT `+squadSelectColumns+`, `+squadMembersAggregate+`
		FROM squads
		WHERE project_id = $1 AND id = $2
	`, projectID, squadID))
}

func (s *Store) ListSquads(ctx context.Context, projectID string) ([]store.Squad, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+squadSelectColumns+`, `+squadMembersAggregate+`
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
		value, err := scanSquadAggregate(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func (s *Store) UpdateSquad(ctx context.Context, input store.Squad) (store.SquadUpdateResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.SquadUpdateResult{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var squadID, previousLeaderAgentID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text, leader_agent_id::text
		FROM squads
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, input.ProjectID, input.ID).Scan(&squadID, &previousLeaderAgentID); err != nil {
		return store.SquadUpdateResult{}, notFound(err)
	}
	if err := lockSquadIdentities(ctx, tx, input); err != nil {
		return store.SquadUpdateResult{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM squad_members WHERE squad_id = $1`, squadID); err != nil {
		return store.SquadUpdateResult{}, err
	}

	value, err := scanSquad(tx.QueryRow(ctx, `
		UPDATE squads
		SET name = $3, leader_agent_id = $4, updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING `+squadSelectColumns+`
	`, input.ProjectID, input.ID, input.Name, input.LeaderAgentID))
	if err != nil {
		return store.SquadUpdateResult{}, err
	}
	if err := insertSquadMembers(ctx, tx, value.ID, input.Members); err != nil {
		return store.SquadUpdateResult{}, err
	}
	payload, err := json.Marshal(struct {
		SquadID string `json:"squadId"`
	}{SquadID: value.ID})
	if err != nil {
		return store.SquadUpdateResult{}, err
	}
	event, err := appendEventTx(ctx, tx, store.Event{
		Type:      "squad.updated",
		ProjectID: value.ProjectID,
		Actor:     store.EmptyObject,
		Payload:   payload,
	})
	if err != nil {
		return store.SquadUpdateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.SquadUpdateResult{}, err
	}
	value.Members = cloneSquadMembers(input.Members)
	return store.SquadUpdateResult{
		Squad:         value,
		LeaderChanged: previousLeaderAgentID != value.LeaderAgentID,
		Events:        []store.Event{event},
	}, nil
}

func (s *Store) DeleteSquad(ctx context.Context, projectID, squadID string) error {
	result, err := s.pool.Exec(ctx, `
		DELETE FROM squads AS sq
		WHERE sq.project_id=$1 AND sq.id=$2
		  AND NOT EXISTS (
			SELECT 1 FROM issues AS i
			WHERE i.project_id=$1 AND i.assignee_type='SQUAD' AND i.assignee_id=$2
		  )
	`, projectID, squadID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 0 {
		return nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM squads WHERE project_id=$1 AND id=$2)`, projectID, squadID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return store.ErrConflict
	}
	return store.ErrNotFound
}

func lockSquadIdentities(ctx context.Context, tx pgx.Tx, value store.Squad) error {
	agentIDs := make([]string, 0, len(value.Members)+1)
	userIDs := make([]string, 0, len(value.Members))
	agentIDs = append(agentIDs, value.LeaderAgentID)
	for _, member := range value.Members {
		switch member.Type {
		case store.SquadMemberTypeAgent:
			agentIDs = append(agentIDs, member.ID)
		case store.SquadMemberTypeUser:
			userIDs = append(userIDs, member.ID)
		default:
			return store.ErrInvalidArgument
		}
	}

	if len(userIDs) > 0 {
		if err := lockProjectWorkflowUserEligibility(ctx, tx); err != nil {
			return err
		}
	}

	sort.Strings(agentIDs)
	previous := ""
	for _, agentID := range agentIDs {
		if agentID == previous {
			continue
		}
		previous = agentID

		var (
			state   string
			inScope bool
		)
		if err := tx.QueryRow(ctx, `
			SELECT state, project_id IS NULL OR project_id = $2
			FROM agents
			WHERE id = $1
			FOR UPDATE
		`, agentID, value.ProjectID).Scan(&state, &inScope); err != nil {
			return notFound(err)
		}
		if state != "ENABLED" || !inScope {
			return store.ErrInvalidArgument
		}
	}

	sort.Strings(userIDs)
	previous = ""
	for _, userID := range userIDs {
		if userID == previous {
			continue
		}
		previous = userID
		if _, err := resolveProjectWorkflowUser(ctx, tx, value.ProjectID, userID); err != nil {
			return err
		}
	}
	return nil
}

func insertSquadMembers(ctx context.Context, tx pgx.Tx, squadID string, members []store.SquadMember) error {
	for _, member := range members {
		var err error
		switch member.Type {
		case store.SquadMemberTypeAgent:
			_, err = tx.Exec(ctx, `
				INSERT INTO squad_members (squad_id, agent_id, role)
				VALUES ($1, $2, $3)
			`, squadID, member.ID, member.Role)
		case store.SquadMemberTypeUser:
			_, err = tx.Exec(ctx, `
				INSERT INTO squad_members (squad_id, user_id, role)
				VALUES ($1, $2, $3)
			`, squadID, member.ID, member.Role)
		default:
			return store.ErrInvalidArgument
		}
		if err != nil {
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

func scanSquadAggregate(row pgx.Row) (store.Squad, error) {
	var (
		value       store.Squad
		membersJSON []byte
	)
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Name, &value.LeaderAgentID, &value.CreatedAt, &value.UpdatedAt, &membersJSON); err != nil {
		return store.Squad{}, notFound(err)
	}
	if err := json.Unmarshal(membersJSON, &value.Members); err != nil {
		return store.Squad{}, err
	}
	return value, nil
}

func cloneSquadMembers(members []store.SquadMember) []store.SquadMember {
	if members == nil {
		return []store.SquadMember{}
	}
	result := make([]store.SquadMember, len(members))
	for i, member := range members {
		result[i] = member
		if member.Role != nil {
			role := *member.Role
			result[i].Role = &role
		}
	}
	return result
}
