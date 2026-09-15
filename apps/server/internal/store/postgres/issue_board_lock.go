package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func lockIssueBoardProject(ctx context.Context, tx pgx.Tx, projectID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('agent-board:issue-board:' || $1, 0))`, projectID)
	return err
}
