package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListDelegationTargets(ctx context.Context, projectID, parentAgentID string) ([]store.DelegationTarget, error) {
	projectID = strings.TrimSpace(projectID)
	parentAgentID = strings.TrimSpace(parentAgentID)
	if projectID == "" || parentAgentID == "" {
		return nil, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id::text, name
		FROM agents
		WHERE id <> $2
		  AND (project_id IS NULL OR project_id=$1)
		ORDER BY lower(name), id
	`, projectID, parentAgentID)
	if err != nil {
		return nil, err
	}
	candidates := make([]store.DelegationTarget, 0)
	for rows.Next() {
		var candidate store.DelegationTarget
		if err := rows.Scan(&candidate.ID, &candidate.Name); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	targets := make([]store.DelegationTarget, 0, len(candidates))
	for _, candidate := range candidates {
		err := s.verifyRunnableAgent(ctx, tx, projectID, candidate.ID)
		switch {
		case err == nil:
			targets = append(targets, candidate)
		case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrNotFound):
			continue
		default:
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return targets, nil
}
