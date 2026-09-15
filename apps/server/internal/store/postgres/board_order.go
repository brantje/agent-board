package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const issueBoardOrderLockPrefix = "agent-board:issue-board:"

func lockIssueBoardOrder(ctx context.Context, tx pgx.Tx, projectID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, issueBoardOrderLockPrefix+projectID)
	return err
}

func newIssueBoardPosition(ctx context.Context, tx pgx.Tx, projectID, status, placement string) (int64, error) {
	switch placement {
	case store.ProjectNewIssuePlacementTop:
		if _, err := tx.Exec(ctx, `
			UPDATE issues
			SET board_position = board_position + 1
			WHERE project_id=$1 AND status=$2
		`, projectID, status); err != nil {
			return 0, err
		}
		return 0, nil
	case store.ProjectNewIssuePlacementBottom:
		var position int64
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(board_position), -1) + 1
			FROM issues
			WHERE project_id=$1 AND status=$2
		`, projectID, status).Scan(&position); err != nil {
			return 0, err
		}
		return position, nil
	default:
		return 0, store.ErrInvalidArgument
	}
}

func placementIndex(ids []string, beforeID, afterID *string) (int, error) {
	if beforeID == nil && afterID == nil {
		if len(ids) == 0 {
			return 0, nil
		}
		return 0, store.ErrConflict
	}
	if beforeID == nil {
		if len(ids) > 0 && afterID != nil && ids[0] == *afterID {
			return 0, nil
		}
		return 0, store.ErrConflict
	}
	if afterID == nil {
		if len(ids) > 0 && ids[len(ids)-1] == *beforeID {
			return len(ids), nil
		}
		return 0, store.ErrConflict
	}
	for index := 1; index < len(ids); index++ {
		if ids[index-1] == *beforeID && ids[index] == *afterID {
			return index, nil
		}
	}
	return 0, store.ErrConflict
}
