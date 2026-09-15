package postgres

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/001_board_order.sql
var boardOrderMigrationSQL string

func runBoardOrderMigration(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, boardOrderMigrationSQL)
	return err
}
